#!/usr/bin/env bash
# The steps of coverreport's composite action (../action.yml), one phase per
# step, in the order action.yml runs them:
#
#   setup    build coverreport from this checkout with the Go on PATH
#   fetch    read the sticky comment back (its hidden state is the push history)
#   analyze  report.json, from the downloaded artifacts and the diff
#   render   the four surfaces, plus a copy of the page under its artifact name
#   (action.yml uploads that page with actions/upload-artifact)
#   publish  render again with the page's URL; annotations, comment, summary
#   check    the gate: exit 1 when a gate fails
#
# Every value arrives in the environment (CR_* for the action's inputs and
# the earlier steps' outputs, GITHUB_* and RUNNER_TEMP from the runner).
# action.yml never interpolates an expression into a script, so an input
# can hold anything without becoming shell code. action_test.go runs
# action.yml's steps as written over testdata/, which is what keeps this file,
# action.yml and the CLI from drifting apart.
set -euo pipefail

# Workflow commands. A message is escaped the way the runner unescapes it
# (%, CR, LF), so a path or a CLI error cannot end the command early or
# start another one.
esc() {
	local s=$1
	s=${s//'%'/'%25'}
	s=${s//$'\r'/'%0D'}
	s=${s//$'\n'/'%0A'}
	printf '%s' "$s"
}
notice() { echo "::notice title=coverreport::$(esc "$1")"; }
warning() { echo "::warning title=coverreport::$(esc "$1")"; }
error() { echo "::error title=coverreport::$(esc "$1")"; }

output() {
	case $2 in
	*$'\n'* | *$'\r'*) error "refusing to write output $1: its value spans lines"; exit 1 ;;
	esac
	printf '%s=%s\n' "$1" "$2" >>"${GITHUB_OUTPUT:-/dev/null}"
}

# gomod_go prints the go directive of the action's go.mod: the oldest Go the
# build accepts. Pure bash, because it runs before anything says PATH is sane.
gomod_go() {
	local k v
	while read -r k v _; do
		if [[ $k == go ]]; then
			printf '%s' "$v"
			return
		fi
	done <"$GITHUB_ACTION_PATH/go.mod"
}

# pr_number prints the pull request number of a pull_request run, the same
# way the CLI finds it (refs/pull/<n>/merge), or nothing.
pr_number() {
	if [[ ${GITHUB_REF:-} =~ ^refs/pull/([0-9]+)/ ]]; then
		printf '%s' "${BASH_REMATCH[1]}"
	fi
}

setup() {
	# The go check comes first and uses only builtins: its whole point is a
	# clear message on a PATH that has nothing on it.
	if ! command -v go >/dev/null 2>&1; then
		error "There is no go on PATH. The action builds coverreport from its own checkout with the Go toolchain already on PATH (Go $(gomod_go) or newer; the module is stdlib-only, so nothing is downloaded). Put one there first: actions/setup-go, or export a dev shell's PATH to \$GITHUB_ENV or \$GITHUB_PATH (see \"Go on PATH\" in the coverreport README)."
		exit 1
	fi
	local tmp=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
	local bin=$tmp/coverreport-bin/coverreport
	mkdir -p "${bin%/*}"
	# Every go command here runs in the action's own directory with its
	# environment pinned, so nothing about the consumer's leaks in:
	#   GOWORK=off         a go.work above the action's checkout must not adopt it
	#   GOTOOLCHAIN=local  an older Go fails, saying so, instead of downloading
	#                      a newer toolchain (and the workspace's own go.mod or
	#                      go.work can never pick one: it is not the directory)
	#   GOPROXY=off        the module has no dependencies, so a build that wants
	#                      the network is a bug and must fail rather than fetch
	#   GOFLAGS=''         a consumer's -mod=vendor or -tags is not ours
	#   CGO_ENABLED=0      nothing here needs cgo, and a nix shell may carry no
	#                      C compiler at all
	gocmd() { (cd "$GITHUB_ACTION_PATH" && GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOFLAGS='' CGO_ENABLED=0 go "$@"); }
	local version
	version=$(gocmd env GOVERSION) || version="the go on PATH"
	if ! gocmd build -o "$bin" ./cmd/coverreport; then
		error "Building coverreport from $GITHUB_ACTION_PATH with $version failed (the error is above). The action needs Go $(gomod_go) or newer on PATH."
		exit 1
	fi
	local dir=${CR_OUT_DIR:-$tmp/coverreport}
	mkdir -p "$dir"
	dir=$(cd "$dir" && pwd)
	rm -f "$dir/comment-failed"
	output bin "$bin"
	output dir "$dir"
	echo "coverreport: built from $GITHUB_ACTION_PATH with $version; writing to $dir"
}

fetch() {
	case $CR_COMMENT in
	true | false) ;;
	*) error "comment must be true or false, not $CR_COMMENT."; exit 1 ;;
	esac
	local prev=$CR_DIR/previous.md
	: >"$prev"
	# Only a pull_request run has a comment to read; publish says why when
	# there is none, once.
	if [[ $CR_COMMENT != true || ${GITHUB_EVENT_NAME:-} != pull_request ]]; then
		return 0
	fi
	# A comment that cannot be read costs the push history, not the report:
	# render starts a new history from an empty file.
	if ! GITHUB_TOKEN=$CR_TOKEN "$CR_BIN" comment --fetch "$prev"; then
		: >"$prev"
		warning "Could not read the sticky comment back (the error is above), so this push starts a new push history. If the upsert below fails the same way, the job's permissions are the cause."
	fi
}

analyze() {
	local root=${CR_ROOT:-.}
	local args=(analyze --root "$root" --config "$CR_CONFIG" --out "$CR_DIR/report.json")
	if [[ -n $CR_ARTIFACTS ]]; then
		if [[ ! -d $CR_ARTIFACTS ]]; then
			warning "$CR_ARTIFACTS does not exist, so no coverage artifact was downloaded there: every layer is \"not measured\", and every required one fails."
		fi
		args+=(--inputs "$CR_ARTIFACTS")
	fi
	if [[ -n $CR_DIFF ]]; then
		args+=(--diff "$CR_DIFF")
	elif [[ -n $CR_DIFF_BASE ]]; then
		if ! command -v git >/dev/null 2>&1; then
			error "diff-base needs git on PATH, and there is none. Put git there, or set diff-base to '' to skip patch coverage."
			exit 1
		fi
		if ! git -C "$root" rev-parse --verify --quiet "$CR_DIFF_BASE^{commit}" >/dev/null; then
			error "diff-base $CR_DIFF_BASE is not a commit in $root. The default, HEAD^1, is the merge commit's first parent, so the checkout needs fetch-depth: 2 (or set diff-base to '' to skip patch coverage)."
			exit 1
		fi
		args+=(--diff-base "$CR_DIFF_BASE")
	fi
	if [[ -n $CR_BASELINE ]]; then
		# The baseline is a cache entry the base branch saves; the first PR
		# after the job lands, or after the entry expires, has none. That is
		# a note, not an error: the layers it would have filled say "not run".
		if [[ -f $CR_BASELINE ]]; then
			args+=(--baseline "$CR_BASELINE")
		else
			notice "No base-branch report at $CR_BASELINE yet, so a layer this run did not measure shows as not run instead of carrying the base branch's numbers."
		fi
	fi
	if [[ -n $CR_ENDPOINTS ]]; then
		args+=(--endpoints "$CR_ENDPOINTS")
	fi
	if [[ -n $CR_REQUIRE ]]; then
		args+=(--require "$CR_REQUIRE")
	fi
	"$CR_BIN" "${args[@]}"
	output report "$CR_DIR/report.json"
}

render() {
	"$CR_BIN" render --report "$CR_REPORT" --out "$CR_DIR" --source-root "${CR_SOURCE_ROOT:-${CR_ROOT:-.}}" --previous "$CR_DIR/previous.md"
	# upload-artifact names an unarchived artifact after its file and ignores
	# its name input, so the page is uploaded as a copy under the name the
	# links will show. The default stays clear of coverage-*, the collectors'
	# artifact pattern: a re-run's download step would otherwise pull the
	# first attempt's page in as an input.
	local name=$CR_PAGE_NAME
	if [[ -z $name ]]; then
		local pr
		pr=$(pr_number)
		name=coverreport-${pr:-${GITHUB_RUN_ID:-local}}.html
	fi
	# A dot file would be dropped by upload-artifact's hidden-file rule.
	if [[ $name == */* || $name == .* || $name == *$'\n'* ]]; then
		error "page-name $name must be a plain file name that does not start with a dot (it becomes the artifact's name)."
		exit 1
	fi
	mkdir -p "$CR_DIR/page"
	cp "$CR_DIR/report.html" "$CR_DIR/page/$name"
	output page "$CR_DIR/page/$name"
	output page-name "$name"
}

publish() {
	# The same render as before plus the page's URL, so the comment, the
	# summary and each annotation link it. The page itself does not change.
	"$CR_BIN" render --report "$CR_REPORT" --out "$CR_DIR" --source-root "${CR_SOURCE_ROOT:-${CR_ROOT:-.}}" --previous "$CR_DIR/previous.md" \
		--artifact-url "$CR_PAGE_URL" --artifact-name "$CR_PAGE_NAME"
	# Annotations are workflow commands on stdout: no checks: write needed.
	cat "$CR_DIR/annotations.txt"
	comment
	cat "$CR_DIR/summary.md" >>"$GITHUB_STEP_SUMMARY"
}

comment() {
	if [[ $CR_COMMENT != true ]]; then
		echo "coverreport: comment is $CR_COMMENT; no sticky comment"
		return 0
	fi
	if [[ ${GITHUB_EVENT_NAME:-} != pull_request ]]; then
		notice "No sticky comment: this is a ${GITHUB_EVENT_NAME:-local} run, not a pull_request one. The job summary carries the report."
		return 0
	fi
	# A fork's pull_request run gets a read-only token, which cannot comment.
	# A head repository that is gone (a deleted fork) is not this one either.
	if [[ ${CR_PR_HEAD_REPO:-} != "${GITHUB_REPOSITORY:-}" ]]; then
		notice "No sticky comment: this pull request comes from ${CR_PR_HEAD_REPO:-a fork that no longer exists}, and a fork's run gets a read-only token. The job summary, the annotations and the page carry the report."
		return 0
	fi
	# A comment that cannot be posted must not hide the gate: say so, publish
	# the rest, and let the check step fail on it at the end.
	if ! GITHUB_TOKEN=$CR_TOKEN "$CR_BIN" comment --body "$CR_DIR/comment.md"; then
		error "The sticky comment could not be posted (the error is above). The summary and the annotations were still published, and the check step fails on this after the gate has run."
		: >"$CR_DIR/comment-failed"
	fi
}

check() {
	local args=(check --root "${CR_ROOT:-.}" --config "$CR_CONFIG" --report "$CR_REPORT")
	if [[ -n $CR_REQUIRE ]]; then
		args+=(--require "$CR_REQUIRE")
	fi
	local out code=0
	out=$("$CR_BIN" "${args[@]}") || code=$?
	printf '%s\n' "$out"
	# One error annotation per failed gate, so the run's page says what broke
	# rather than only "exit code 1".
	local line
	while IFS= read -r line; do
		if [[ $line == '  - '* ]]; then
			error "${line#  - }"
		fi
	done <<<"$out"
	if [[ -f $CR_DIR/comment-failed ]]; then
		error "The sticky comment could not be posted (see the publish step), so this step fails even where every gate held."
		if ((code == 0)); then
			code=1
		fi
	fi
	exit "$code"
}

case ${1:-} in
setup | fetch | analyze | render | publish | check) "$1" ;;
*)
	echo "usage: run.sh setup|fetch|analyze|render|publish|check" >&2
	exit 2
	;;
esac
