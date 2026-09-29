// Package ghapi is the few GitHub REST calls the sticky comment needs:
// list a pull request's issue comments (every page), create one, update
// one. Standard library only, so a repository runs it with nothing but a
// Go toolchain and the job's GITHUB_TOKEN.
//
// The token is sent in the Authorization header and nowhere else: it is
// never part of an error, a log line or a URL, and every error string is
// scrubbed of it as a last line of defence (an http.Client error can echo a
// request, and a proxy can echo anything).
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultAPI is api.github.com; GitHub Enterprise Server and tests pass
// their own (Actions sets GITHUB_API_URL).
const DefaultAPI = "https://api.github.com"

// Client calls the GitHub REST API.
type Client struct {
	API   string
	Token string
	HTTP  *http.Client
	// UserAgent is required by GitHub.
	UserAgent string
	// Retries is how many times an idempotent request (GET, PATCH) is
	// retried after a 5xx or a network error. POST is never retried: a
	// timeout after the comment was created would post it twice.
	Retries int
	// Sleep waits between retries (injectable for tests).
	Sleep func(time.Duration)
}

// Comment is an issue comment.
type Comment struct {
	ID      int64  `json:"id"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// Error is a GitHub API failure with a message a CI log reader can act on.
type Error struct {
	Status  int
	Method  string
	Path    string
	Message string // GitHub's own message, when it sent one
	Hint    string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("GitHub API %s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.Hint != "" {
		s += ". " + e.Hint
	}
	return s
}

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// ValidRepo reports whether s is owner/name.
func ValidRepo(s string) bool { return repoRE.MatchString(s) && !strings.Contains(s, "..") }

func (c *Client) scrub(s string) string {
	if c.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.Token, "***")
}

// do sends one request and decodes a JSON response into out (when non-nil).
// It returns the response headers for pagination.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	attempts := 1
	if method != http.MethodPost {
		attempts += c.Retries
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && c.Sleep != nil {
			c.Sleep(time.Duration(i) * time.Second)
		}
		h, err, retry := c.once(ctx, method, path, payload, out)
		if err == nil {
			return h, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, method, path string, payload []byte, out any) (http.Header, error, bool) {
	u := strings.TrimRight(c.API, "/") + path
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, errors.New(c.scrub(err.Error())), false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API %s %s: %s", method, path, c.scrub(err.Error())), true
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("GitHub API %s %s: reading the response: %s", method, path, c.scrub(err.Error())), true
	}
	if resp.StatusCode/100 != 2 {
		e := &Error{Status: resp.StatusCode, Method: method, Path: path}
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &m) == nil {
			e.Message = c.scrub(m.Message)
		}
		e.Hint = hint(resp.StatusCode, method, resp.Header)
		return nil, e, resp.StatusCode >= 500
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("GitHub API %s %s: decoding the response: %s", method, path, c.scrub(err.Error())), false
		}
	}
	return resp.Header, nil, false
}

// hint says what to do about a status, in the terms of a workflow file.
func hint(status int, method string, h http.Header) string {
	switch status {
	case http.StatusUnauthorized:
		return "The token was refused: pass the job's GITHUB_TOKEN (or a token that is still valid) in the GITHUB_TOKEN environment variable"
	case http.StatusForbidden:
		if h.Get("X-RateLimit-Remaining") == "0" {
			return "The API rate limit is exhausted; retry after " + h.Get("X-RateLimit-Reset") + " (Unix time)"
		}
		if method == http.MethodGet {
			return "The token cannot read this pull request's comments: the job needs `permissions: pull-requests: read` at least"
		}
		return "The token may not write comments: give the job `permissions: pull-requests: write`. On a pull request from a fork, GITHUB_TOKEN is read-only and cannot comment at all"
	case http.StatusNotFound:
		return "Check --repo and --pr, and that the token can see the repository (a private repository answers 404 to a token without access, not 403)"
	case http.StatusUnprocessableEntity:
		return "GitHub rejected the body; a comment is at most 65,536 characters"
	}
	if status >= 500 {
		return "GitHub had a server error; the request was retried where it was safe to"
	}
	return ""
}

var nextRE = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// Comments lists every comment on an issue or pull request, following the
// Link header's rel="next" until the last page.
func (c *Client) Comments(ctx context.Context, repo string, number int) ([]Comment, error) {
	path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, number)
	var all []Comment
	for page := 0; path != ""; page++ {
		if page == 1000 {
			return nil, errors.New("GitHub API: more than 1000 pages of comments; refusing to walk further")
		}
		var batch []Comment
		h, err := c.do(ctx, http.MethodGet, path, nil, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		path = ""
		if m := nextRE.FindStringSubmatch(h.Get("Link")); m != nil {
			next, err := url.Parse(m[1])
			if err != nil {
				return nil, fmt.Errorf("GitHub API: bad Link header: %w", err)
			}
			// Follow only the path and query: a Link pointing at another
			// host must not receive the token.
			path = next.EscapedPath()
			if next.RawQuery != "" {
				path += "?" + next.RawQuery
			}
			if base, err := url.Parse(c.API); err == nil && base.Path != "" && base.Path != "/" {
				path = strings.TrimPrefix(path, strings.TrimRight(base.Path, "/"))
			}
		}
	}
	return all, nil
}

// Create posts a new comment.
func (c *Client) Create(ctx context.Context, repo string, number int, body string) (*Comment, error) {
	var out Comment
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]string{"body": body}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Update replaces a comment's body.
func (c *Client) Update(ctx context.Context, repo string, id int64, body string) (*Comment, error) {
	var out Comment
	_, err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/comments/%s", repo, strconv.FormatInt(id, 10)), map[string]string{"body": body}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Find returns the first (oldest) comment whose body starts with marker,
// how many comments carry it, and every comment it read.
func Find(cs []Comment, marker string) (*Comment, int) {
	var first *Comment
	n := 0
	for i := range cs {
		if strings.HasPrefix(strings.TrimLeft(cs[i].Body, " \t\r\n"), marker) {
			n++
			if first == nil {
				first = &cs[i]
			}
		}
	}
	return first, n
}

// Result is what an upsert did.
type Result struct {
	Created  bool
	Comment  *Comment
	Previous string // the body it replaced ("" when it created one)
	// Duplicates counts further comments carrying the marker, left alone.
	Duplicates int
}

// Upsert updates the comment carrying marker, or creates one. The body
// must itself start with the marker, or the next run could not find it.
func (c *Client) Upsert(ctx context.Context, repo string, number int, marker, body string) (*Result, error) {
	if !strings.HasPrefix(body, marker) {
		return nil, fmt.Errorf("the comment body must start with its marker %q, or the next run could not find it", marker)
	}
	cs, err := c.Comments(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	old, n := Find(cs, marker)
	if old == nil {
		created, err := c.Create(ctx, repo, number, body)
		if err != nil {
			return nil, err
		}
		return &Result{Created: true, Comment: created}, nil
	}
	updated, err := c.Update(ctx, repo, old.ID, body)
	if err != nil {
		return nil, err
	}
	return &Result{Comment: updated, Previous: old.Body, Duplicates: n - 1}, nil
}
