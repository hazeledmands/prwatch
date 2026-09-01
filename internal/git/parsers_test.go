package git

import (
	"testing"
	"time"
)

// These tests pin the *behavior* of the three parsers — input in, parsed value
// out — rather than their internals. They were written as the safety net for
// the A6 conversion to NUL-delimited (`-z`) git output, which has now landed:
// the two path parsers take the NUL records produced by `runZ` rather than a
// newline-delimited blob, and the cases that previously recorded
// core.quotePath's octal-escaped paths as `CURRENT BEHAVIOR:` now assert the
// raw UTF-8 the `-z` pipeline delivers.
//
// The record shapes below were verified against real git output, not inferred:
// `diff --name-status -z` emits `R100\0old\0new\0`, and `status --porcelain=v2
// -z` emits a rename entry's original path as a *separate* following record.

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

// z builds the record slice runZ would hand a parser for the given raw -z
// stdout, so the test tables exercise the real split as well as the parser.
func z(fields ...string) []string {
	var raw string
	for _, f := range fields {
		raw += f + "\x00"
	}
	return splitNUL(raw)
}

func TestParseRenameNameStatus(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []Rename
	}{
		{
			name: "empty input",
			in:   z(),
			want: nil,
		},
		{
			name: "only empty records",
			in:   z("", "", ""),
			want: nil,
		},
		{
			name: "pure rename score 100",
			in:   z("R100", "old.go", "new.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename with edits score 087",
			in:   z("R087", "old.go", "new.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			name: "score with leading zeros parses",
			in:   z("R099", "a", "b"),
			want: []Rename{{Old: "a", New: "b", Pure: false}},
		},
		{
			name: "bare R with no score is treated as score 0",
			in:   z("R", "old.go", "new.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			// Each single-path status consumes exactly one path record, so
			// the trailing rename is still found at the right offset.
			name: "non-rename statuses are skipped",
			in:   z("M", "modified.go", "A", "added.go", "D", "deleted.go", "R100", "old", "new"),
			want: []Rename{{Old: "old", New: "new", Pure: true}},
		},
		{
			// C is two-path like R, so it must consume both records even
			// though the entry itself is discarded.
			name: "copies are skipped (C is not R) but still consume two paths",
			in:   z("C100", "src.go", "copy.go", "R100", "a", "b"),
			want: []Rename{{Old: "a", New: "b", Pure: true}},
		},
		{
			name: "multiple renames",
			in:   z("R100", "a", "b", "R050", "c", "d"),
			want: []Rename{
				{Old: "a", New: "b", Pure: true},
				{Old: "c", New: "d", Pure: false},
			},
		},
		{
			name: "spaces in filenames survive",
			in:   z("R100", "old name.go", "new name.go"),
			want: []Rename{{Old: "old name.go", New: "new name.go", Pure: true}},
		},
		{
			name: "malformed: rename status with only one path",
			in:   z("R100", "only-old.go"),
			want: nil,
		},
		{
			name: "malformed: status token with no path at all",
			in:   z("R100"),
			want: nil,
		},
		{
			name: "unknown single-path status does not abort the rest",
			in:   z("X", "garbage.go", "R100", "a", "b"),
			want: []Rename{{Old: "a", New: "b", Pure: true}},
		},
		{
			// A6: -z suppresses core.quotePath, so non-ASCII paths arrive as
			// raw UTF-8 rather than the octal-escaped `"caf\303\251.txt"`.
			name: "non-ASCII path arrives as raw UTF-8",
			in:   z("R100", "café.txt", "coffee.txt"),
			want: []Rename{{Old: "café.txt", New: "coffee.txt", Pure: true}},
		},
		{
			// A6: the tab is no longer a delimiter, so a filename containing
			// one survives whole instead of being quoted or split.
			name: "path containing a literal tab survives whole",
			in:   z("R100", "a\tb.go", "c.go"),
			want: []Rename{{Old: "a\tb.go", New: "c.go", Pure: true}},
		},
		{
			// A6: an embedded tab in the *new* path no longer truncates it.
			name: "literal tab in the new path does not truncate it",
			in:   z("R100", "old.go", "new\tname.go"),
			want: []Rename{{Old: "old.go", New: "new\tname.go", Pure: true}},
		},
		{
			// A newline is likewise just a byte in a path now.
			name: "path containing a newline survives whole",
			in:   z("R100", "a\nb.go", "c.go"),
			want: []Rename{{Old: "a\nb.go", New: "c.go", Pure: true}},
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
	// Header record: 2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <newPath>
	// followed by a separate record holding <origPath>.
	const hdr = "2 R. N... 100644 100644 100644 aaaaaaa bbbbbbb "

	tests := []struct {
		name string
		in   []string
		want []Rename
	}{
		{
			name: "empty input",
			in:   z(),
			want: nil,
		},
		{
			name: "pure rename",
			in:   z(hdr+"R100 new.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename with edits",
			in:   z(hdr+"R075 new.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: false}},
		},
		{
			name: "spaces in new path are preserved",
			in:   z(hdr+"R100 new name with spaces.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new name with spaces.go", Pure: true}},
		},
		{
			name: "spaces in orig path are preserved",
			in:   z(hdr+"R100 new.go", "old name.go"),
			want: []Rename{{Old: "old name.go", New: "new.go", Pure: true}},
		},
		{
			name: "rename flagged in Y position",
			in:   z("2 .R N... 100644 100644 100644 aaaaaaa bbbbbbb R100 new.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "copies are skipped",
			in:   z("2 C. N... 100644 100644 100644 aaaaaaa bbbbbbb C100 copy.go", "src.go"),
			want: nil,
		},
		{
			name: "R in XY but C in the score field is skipped",
			in:   z(hdr+"C100 copy.go", "src.go"),
			want: nil,
		},
		{
			// A skipped type-2 entry must still consume its origPath record,
			// or the following entry is read at the wrong offset.
			name: "a skipped copy still consumes its orig-path record",
			in:   z(hdr+"C100 copy.go", "src.go", hdr+"R100 new.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "non-type-2 records are skipped",
			in: z("1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb modified.go",
				"? untracked.go",
				"# branch.oid abcdef",
				hdr+"R100 new.go", "old.go"),
			want: []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name: "malformed: rename entry with no orig-path record",
			in:   z(hdr + "R100 new.go"),
			want: nil,
		},
		{
			name: "malformed: too few header fields",
			in:   z("2 R. N... 100644 R100 new.go", "old.go"),
			want: nil,
		},
		{
			name: "malformed: type-2 prefix with nothing else",
			in:   z("2 "),
			want: nil,
		},
		{
			name: "multiple renames",
			in:   z(hdr+"R100 a", "b", hdr+"R050 c", "d"),
			want: []Rename{
				{Old: "b", New: "a", Pure: true},
				{Old: "d", New: "c", Pure: false},
			},
		},
		{
			// A6: -z delivers both paths as raw UTF-8 rather than
			// core.quotePath's quoted, octal-escaped form.
			name: "non-ASCII paths arrive as raw UTF-8",
			in:   z(hdr+"R100 café.txt", "thé.txt"),
			want: []Rename{{Old: "thé.txt", New: "café.txt", Pure: true}},
		},
		{
			// A6: the tab that used to separate the two paths on one line is
			// now just a byte a path may contain.
			name: "paths containing tabs survive whole",
			in:   z(hdr+"R100 new\tname.go", "old\tname.go"),
			want: []Rename{{Old: "old\tname.go", New: "new\tname.go", Pure: true}},
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
