package ghapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const token = "ghs_exampleTOKEN1234567890"

const marker = "<!-- coverreport:v1 -->"

// fake is an in-memory GitHub issue-comments API: pages of size perPage,
// Link headers, create and update, and failure injection.
type fake struct {
	t       *testing.T
	mu      sync.Mutex
	perPage int
	cs      []Comment
	nextID  int64
	calls   []string
	// fail, when set, answers the n-th request of a method with a status
	// (counting from 1), once.
	fail map[string]map[int]int
	// failBody is the JSON body failures carry.
	failBody string
	// linkHost overrides the host in Link headers.
	linkHost string
	seen     map[string]int
}

func newFake(t *testing.T, bodies ...string) *fake {
	f := &fake{t: t, perPage: 2, nextID: 100, fail: map[string]map[int]int{}, seen: map[string]int{}}
	for _, b := range bodies {
		f.nextID++
		c := Comment{ID: f.nextID, Body: b, HTMLURL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", f.nextID)}
		f.cs = append(f.cs, c)
	}
	return f
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
	f.seen[r.Method]++
	if r.Header.Get("Authorization") != "Bearer "+token {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"Bad credentials"}`)
		return
	}
	if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
		f.t.Errorf("%s %s: missing a required header: %v", r.Method, r.URL, r.Header)
	}
	if st, ok := f.fail[r.Method][f.seen[r.Method]]; ok {
		w.WriteHeader(st)
		io.WriteString(w, f.failBody)
		return
	}
	switch {
	// GitHub's own Link headers point at /repositories/<id>/...: both
	// spellings name the same list.
	case r.Method == http.MethodGet && (r.URL.Path == "/repos/o/r/issues/7/comments" || r.URL.Path == "/repositories/42/issues/7/comments"):
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		lo, hi := (page-1)*f.perPage, page*f.perPage
		if lo > len(f.cs) {
			lo = len(f.cs)
		}
		if hi > len(f.cs) {
			hi = len(f.cs)
		}
		if hi < len(f.cs) {
			host := f.linkHost
			if host == "" {
				host = "http://" + r.Host
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/repositories/42/issues/7/comments?per_page=100&page=%d>; rel="next", <%s/x?page=9>; rel="last"`, host, page+1, host))
		}
		json.NewEncoder(w).Encode(f.cs[lo:hi])
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		f.nextID++
		c := Comment{ID: f.nextID, Body: in.Body, HTMLURL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", f.nextID)}
		f.cs = append(f.cs, c)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(c)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), 10, 64)
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		for i := range f.cs {
			if f.cs[i].ID == id {
				f.cs[i].Body = in.Body
				json.NewEncoder(w).Encode(f.cs[i])
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	}
}

func client(t *testing.T, f *fake) (*Client, func()) {
	srv := httptest.NewServer(f)
	return &Client{API: srv.URL, Token: token, HTTP: srv.Client(), UserAgent: "coverreport-test", Retries: 2, Sleep: func(time.Duration) {}}, srv.Close
}

func TestUpsertCreatesWhenNoCommentCarriesTheMarker(t *testing.T) {
	f := newFake(t, "LGTM", "<!-- someone-else:v1 -->\nnot ours")
	c, done := client(t, f)
	defer done()
	res, err := c.Upsert(context.Background(), "o/r", 7, marker, marker+"\nfirst")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Previous != "" || len(f.cs) != 3 || f.cs[2].Body != marker+"\nfirst" {
		t.Fatalf("created %+v; comments %+v", res, f.cs)
	}
	if f.cs[0].Body != "LGTM" || f.cs[1].Body != "<!-- someone-else:v1 -->\nnot ours" {
		t.Error("an unrelated comment was changed")
	}
}

// The marker comment sits on the third page of five: the upsert must walk
// the Link headers to find it, update exactly it, and return what it said.
func TestUpsertUpdatesTheMarkedCommentAcrossPages(t *testing.T) {
	f := newFake(t, "a", "b", "c", "d", marker+"\n<!-- coverreport:state {} -->\nold", "e", "f", marker+"\nduplicate", "g")
	c, done := client(t, f)
	defer done()
	res, err := c.Upsert(context.Background(), "o/r", 7, marker, marker+"\nnew")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.Comment.ID != 105 || res.Previous != marker+"\n<!-- coverreport:state {} -->\nold" || res.Duplicates != 1 {
		t.Fatalf("result %+v", res)
	}
	if f.cs[4].Body != marker+"\nnew" || f.cs[7].Body != marker+"\nduplicate" {
		t.Error("updated the wrong comment")
	}
	gets := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, "GET ") {
			gets++
		}
	}
	if gets != 5 {
		t.Errorf("walked %d pages, want all 5: %v", gets, f.calls)
	}
}

func TestUpsertRefusesABodyWithoutItsMarker(t *testing.T) {
	f := newFake(t)
	c, done := client(t, f)
	defer done()
	if _, err := c.Upsert(context.Background(), "o/r", 7, marker, "no marker\n"+marker); err == nil {
		t.Fatal("a body the next run could not find was posted")
	}
	if len(f.calls) != 0 {
		t.Errorf("called the API anyway: %v", f.calls)
	}
}

// A 403 on the write names the permission; a 404 names the flags; the
// token never appears in an error, even when GitHub echoes it back.
func TestErrorsSayWhatToDoAndNeverCarryTheToken(t *testing.T) {
	for _, tc := range []struct {
		method string
		status int
		want   string
	}{
		{http.MethodPost, 403, "pull-requests: write"},
		{http.MethodGet, 403, "pull-requests: read"},
		{http.MethodGet, 404, "Check --repo and --pr"},
		{http.MethodPost, 422, "65,536 characters"},
		{http.MethodGet, 401, "The token was refused"},
	} {
		f := newFake(t)
		f.fail[tc.method] = map[int]int{1: tc.status, 2: tc.status, 3: tc.status}
		f.failBody = `{"message":"Denied for ` + token + `"}`
		c, done := client(t, f)
		_, err := c.Upsert(context.Background(), "o/r", 7, marker, marker+"\nx")
		done()
		if err == nil {
			t.Fatalf("%s %d: no error", tc.method, tc.status)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %d: %q lacks %q", tc.method, tc.status, err, tc.want)
		}
		if strings.Contains(err.Error(), token) {
			t.Errorf("%s %d: the token leaked into %q", tc.method, tc.status, err)
		}
		var ge *Error
		if !asError(err, &ge) || ge.Status != tc.status {
			t.Errorf("%s %d: not an *Error with the status: %#v", tc.method, tc.status, err)
		}
	}
	// A wrong token is 401 from the server's own check.
	f := newFake(t)
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Client{API: srv.URL, Token: "wrong", HTTP: srv.Client(), UserAgent: "x"}
	if _, err := c.Comments(context.Background(), "o/r", 7); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("bad token: %v", err)
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestRateLimitIsNamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1790600000")
		w.WriteHeader(403)
		io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer srv.Close()
	c := &Client{API: srv.URL, Token: token, HTTP: srv.Client(), UserAgent: "x"}
	_, err := c.Comments(context.Background(), "o/r", 7)
	if err == nil || !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "1790600000") {
		t.Errorf("rate limit: %v", err)
	}
}

// A 5xx on a read is retried; a 5xx on the create is not, because a create
// that timed out after succeeding would post the comment twice.
func TestRetriesIdempotentRequestsOnly(t *testing.T) {
	f := newFake(t, marker+"\nold")
	f.fail[http.MethodGet] = map[int]int{1: 502}
	f.fail[http.MethodPatch] = map[int]int{1: 503}
	c, done := client(t, f)
	res, err := c.Upsert(context.Background(), "o/r", 7, marker, marker+"\nnew")
	done()
	if err != nil || res.Previous != marker+"\nold" {
		t.Fatalf("retry did not recover: %v %+v", err, res)
	}
	if f.seen[http.MethodGet] != 2 || f.seen[http.MethodPatch] != 2 {
		t.Errorf("attempts: %v", f.seen)
	}

	f = newFake(t)
	f.fail[http.MethodPost] = map[int]int{1: 502}
	c, done = client(t, f)
	_, err = c.Upsert(context.Background(), "o/r", 7, marker, marker+"\nnew")
	done()
	if err == nil || f.seen[http.MethodPost] != 1 {
		t.Errorf("POST retried (%d attempts) or error lost: %v", f.seen[http.MethodPost], err)
	}
}

// A Link header pointing at another host is followed on our own API host:
// the token only ever goes where the client was configured to send it.
func TestPaginationNeverFollowsAnotherHost(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the other host was called: %s %s (Authorization %q)", r.Method, r.URL, r.Header.Get("Authorization"))
	}))
	defer evil.Close()
	f := newFake(t, "a", "b", marker+"\nold")
	f.linkHost = evil.URL
	c, done := client(t, f)
	defer done()
	cs, err := c.Comments(context.Background(), "o/r", 7)
	if err != nil || len(cs) != 3 {
		t.Fatalf("%v, %d comments", err, len(cs))
	}
}

func TestValidRepo(t *testing.T) {
	for _, s := range []string{"o/r", "handixyz/handipay", "a-b/c.d_e"} {
		if !ValidRepo(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "o", "o/r/x", "../x", "o/..", "o r/x", "o/r?x=1"} {
		if ValidRepo(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
