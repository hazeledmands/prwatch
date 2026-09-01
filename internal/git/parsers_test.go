package git

import (
	"testing"
	"time"
)

// These tests pin the *behavior* of the three line parsers — input string in,
// parsed value out — rather than their internals. They previously had no
// dedicated coverage at all, only incidental exercise through happy-path
// integration tests, which meant no safety net for the planned A6 conversion
// to NUL-delimited (`-z`) git output.
//
// Where current behavior is plainly wrong for exotic input (git's default
// core.quotePath octal-escaping of non-ASCII paths, embedded tabs) the case is
// recorded as-is with an `// A6:` note saying what should change, so the
// conversion has a visible before/after rather than a silent one.

func renamesEqual(a, b []Rename) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseRenameNameStatus(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Rename
	}{
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "only newlines",
			in:   "\n\n\n",
			want: nil,
		},
		{
			name: "pure rename score 100",
			in:   "R100\told.go\tnew.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename with edits score 087",
			in:   "R087\told.go\tnew.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			name: "score with leading zeros parses",
			in:   "R099\ta\tb\n",
			want: []Rename{{Old: "a", New: "b", Pure: false}},
		},
		{
			name: "bare R with no score is treated as score 0",
			in:   "R\told.go\tnew.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			name: "non-rename statuses are skipped",
			in:   "M\tmodified.go\nA\tadded.go\nD\tdeleted.go\nR100\told\tnew\n",
			want: []Rename{{Old: "old", New: "new", Pure: true}},
		},
		{
			name: "copies are skipped (C is not R)",
			in:   "C100\tsrc.go\tcopy.go\n",
			want: nil,
		},
		{
			name: "multiple renames",
			in:   "R100\ta\tb\nR050\tc\td\n",
			want: []Rename{
				{Old: "a", New: "b", Pure: true},
				{Old: "c", New: "d", Pure: false},
			},
		},
		{
			name: "spaces in filenames survive (tab is the delimiter)",
			in:   "R100\told name.go\tnew name.go\n",
			want: []Rename{{Old: "old name.go", New: "new name.go", Pure: true}},
		},
		{
			name: "malformed: only two fields",
			in:   "R100\tonly-old.go\n",
			want: nil,
		},
		{
			name: "malformed: no tabs at all",
			in:   "R100 old.go new.go\n",
			want: nil,
		},
		{
			name: "malformed line does not abort the rest",
			in:   "garbage\nR100\ta\tb\n",
			want: []Rename{{Old: "a", New: "b", Pure: true}},
		},
		{
			// A6: git quotes non-ASCII paths by default (core.quotePath),
			// emitting `"caf\303\251.txt"`. The parser passes the quoted,
			// octal-escaped form straight through, so the UI shows the escape
			// sequence rather than "café.txt". Converting to `git -c
			// core.quotePath=false ... -z` should make this case produce
			// {Old: "café.txt", New: "coffee.txt"}.
			name: "CURRENT BEHAVIOR: quoted non-ASCII path is not unescaped",
			in:   "R100\t\"caf\\303\\251.txt\"\tcoffee.txt\n",
			want: []Rename{{Old: `"caf\303\251.txt"`, New: "coffee.txt", Pure: true}},
		},
		{
			// A6: same quoting path — a filename containing a literal tab is
			// emitted by git as `"a\tb.go"` (quoted, backslash-escaped), which
			// the parser leaves quoted. With -z the raw name would come
			// through intact.
			name: "CURRENT BEHAVIOR: quoted path with escaped tab stays quoted",
			in:   "R100\t\"a\\tb.go\"\tc.go\n",
			want: []Rename{{Old: `"a\tb.go"`, New: "c.go", Pure: true}},
		},
		{
			// A6: an *unquoted* embedded tab (what -z output would allow, or
			// what a hand-rolled fixture looks like) splits into extra fields
			// and silently truncates the new path.
			name: "CURRENT BEHAVIOR: unquoted tab in path truncates the new path",
			in:   "R100\told.go\tnew\tname.go\n",
			want: []Rename{{Old: "old.go", New: "new", Pure: true}},
		},
		{
			name: "trailing newline absent",
			in:   "R100\ta\tb",
			want: []Rename{{Old: "a", New: "b", Pure: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRenameNameStatus(tt.in)
			if !renamesEqual(got, tt.want) {
				t.Errorf("parseRenameNameStatus(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePorcelainV2Renames(t *testing.T) {
	// Header shape: 2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <newPath>\t<origPath>
	const hdr = "2 R. N... 100644 100644 100644 aaaaaaa bbbbbbb "

	tests := []struct {
		name string
		in   string
		want []Rename
	}{
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "pure rename",
			in:   hdr + "R100 new.go\told.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename with edits",
			in:   hdr + "R075 new.go\told.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			name: "spaces in new path are preserved",
			in:   hdr + "R100 new name with spaces.go\told.go\n",
			want: []Rename{{Old: "old.go", New: "new name with spaces.go", Pure: true}},
		},
		{
			name: "spaces in orig path are preserved",
			in:   hdr + "R100 new.go\told name.go\n",
			want: []Rename{{Old: "old name.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename flagged in Y position",
			in:   "2 .R N... 100644 100644 100644 aaaaaaa bbbbbbb R100 new.go\told.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "copies are skipped",
			in:   "2 C. N... 100644 100644 100644 aaaaaaa bbbbbbb C100 copy.go\tsrc.go\n",
			want: nil,
		},
		{
			name: "R in XY but C in the score field is skipped",
			in:   hdr + "C100 copy.go\tsrc.go\n",
			want: nil,
		},
		{
			name: "non-type-2 lines are skipped",
			in: "1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb modified.go\n" +
				"? untracked.go\n" +
				"# branch.oid abcdef\n" +
				hdr + "R100 new.go\told.go\n",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "malformed: no tab separator",
			in:   hdr + "R100 new.go\n",
			want: nil,
		},
		{
			name: "malformed: too few header fields",
			in:   "2 R. N... 100644 R100 new.go\told.go\n",
			want: nil,
		},
		{
			name: "malformed: type-2 prefix with nothing else",
			in:   "2 \n",
			want: nil,
		},
		{
			name: "multiple renames",
			in:   hdr + "R100 a\tb\n" + hdr + "R050 c\td\n",
			want: []Rename{
				{Old: "b", New: "a", Pure: true},
				{Old: "d", New: "c", Pure: false},
			},
		},
		{
			// A6: porcelain v2 also honours core.quotePath, so a non-ASCII
			// path arrives quoted and octal-escaped and is passed through
			// verbatim. `-z` output (which porcelain v2 supports natively)
			// would deliver the raw bytes and NUL-separate the two paths.
			name: "CURRENT BEHAVIOR: quoted non-ASCII paths are not unescaped",
			in:   hdr + "R100 \"caf\\303\\251.txt\"\t\"the\\303\\251.txt\"\n",
			want: []Rename{{Old: `"the\303\251.txt"`, New: `"caf\303\251.txt"`, Pure: true}},
		},
		{
			name: "no trailing newline",
			in:   hdr + "R100 new.go\told.go",
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePorcelainV2Renames(tt.in)
			if !renamesEqual(got, tt.want) {
				t.Errorf("parsePorcelainV2Renames(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseCommitLog(t *testing.T) {
	mustTime := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("bad test time %q: %v", s, err)
		}
		return tm
	}

	tests := []struct {
		name string
		in   string
		want []Commit
	}{
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "blank lines only",
			in:   "\n\n",
			want: nil,
		},
		{
			name: "full record",
			in:   "abc123\tHazel\t2026-08-31T10:00:00Z\tfix the thing\n",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "Hazel",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
				Subject:    "fix the thing",
			}},
		},
		{
			name: "subject containing tabs is kept whole (SplitN limit 4)",
			in:   "abc123\tHazel\t2026-08-31T10:00:00Z\tfix\tthe\tthing\n",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "Hazel",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
				Subject:    "fix\tthe\tthing",
			}},
		},
		{
			name: "CRLF line endings are trimmed",
			in:   "abc123\tHazel\t2026-08-31T10:00:00Z\tsubject\r\n",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "Hazel",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
				Subject:    "subject",
			}},
		},
		{
			name: "unparseable date leaves a zero time, other fields survive",
			in:   "abc123\tHazel\tnot-a-date\tsubject\n",
			want: []Commit{{SHA: "abc123", Author: "Hazel", Subject: "subject"}},
		},
		{
			name: "SHA only",
			in:   "abc123\n",
			want: []Commit{{SHA: "abc123"}},
		},
		{
			name: "SHA and author only",
			in:   "abc123\tHazel\n",
			want: []Commit{{SHA: "abc123", Author: "Hazel"}},
		},
		{
			name: "empty subject",
			in:   "abc123\tHazel\t2026-08-31T10:00:00Z\t\n",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "Hazel",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
			}},
		},
		{
			name: "author name containing spaces and unicode",
			in:   "abc123\tRené Müller\t2026-08-31T10:00:00Z\tsübject 日本\n",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "René Müller",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
				Subject:    "sübject 日本",
			}},
		},
		{
			name: "multiple commits, blank line between",
			in: "aaa\tA\t2026-08-31T10:00:00Z\tone\n" +
				"\n" +
				"bbb\tB\t2026-08-30T10:00:00Z\ttwo\n",
			want: []Commit{
				{SHA: "aaa", Author: "A", AuthorDate: mustTime("2026-08-31T10:00:00Z"), Subject: "one"},
				{SHA: "bbb", Author: "B", AuthorDate: mustTime("2026-08-30T10:00:00Z"), Subject: "two"},
			},
		},
		{
			// A line with no tabs at all still yields a Commit whose SHA is
			// the whole line. Documented rather than asserted-as-desired: the
			// parser has no validation, so garbage in the log stream becomes a
			// bogus commit entry.
			name: "CURRENT BEHAVIOR: a tabless line becomes a SHA-only commit",
			in:   "this is not a commit line\n",
			want: []Commit{{SHA: "this is not a commit line"}},
		},
		{
			name: "no trailing newline",
			in:   "abc123\tHazel\t2026-08-31T10:00:00Z\tsubject",
			want: []Commit{{
				SHA:        "abc123",
				Author:     "Hazel",
				AuthorDate: mustTime("2026-08-31T10:00:00Z"),
				Subject:    "subject",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCommitLog(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("parseCommitLog(%q) returned %d commits, want %d: %+v", tt.in, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].SHA != tt.want[i].SHA ||
					got[i].Author != tt.want[i].Author ||
					got[i].Subject != tt.want[i].Subject ||
					!got[i].AuthorDate.Equal(tt.want[i].AuthorDate) {
					t.Errorf("commit %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
