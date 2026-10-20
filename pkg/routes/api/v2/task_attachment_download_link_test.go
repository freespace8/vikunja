// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package apiv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeDownloadName(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
	}{
		"plain": {
			in:   "image.png",
			want: "image.png",
		},
		"keeps spaces and unicode": {
			in:   "Bericht für März.pdf",
			want: "Bericht für März.pdf",
		},
		"unix path": {
			in:   "../../etc/passwd",
			want: "passwd",
		},
		"windows path": {
			in:   `C:\Users\me\secret.txt`,
			want: "secret.txt",
		},
		"control characters": {
			in:   "a\nb\x00c\td.txt",
			want: "abcd.txt",
		},
		"output template": {
			in:   "a#1.txt",
			want: "a_1.txt",
		},
		"stdout": {
			in:   "-",
			want: "_-",
		},
		"leading dash": {
			in:   "-rf.txt",
			want: "_-rf.txt",
		},
		"empty": {
			in:   "",
			want: "attachment",
		},
		"only a separator": {
			in:   "dir/",
			want: "attachment",
		},
		"dot": {
			in:   ".",
			want: "attachment",
		},
		"dot dot": {
			in:   "..",
			want: "attachment",
		},
		"only whitespace": {
			in:   "  \t ",
			want: "attachment",
		},
		"format characters": {
			in:   "\u202egpj.exe",
			want: "gpj.exe",
		},
		"long name": {
			in:   strings.Repeat("ä", 150) + ".txt",
			want: strings.Repeat("ä", 100),
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, safeDownloadName(tc.in))
		})
	}
}

func TestLocalDownloadName(t *testing.T) {
	assert.Equal(t, "vikunja-4-.bashrc", localDownloadName(4, ".bashrc"))
	assert.Equal(t, "vikunja-4-_-rf", localDownloadName(4, "-rf"))
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, `'plain'`, shellQuote("plain"))
	assert.Equal(t, `'$(rm -rf ~) `+"`id`"+` "x" \n'`, shellQuote(`$(rm -rf ~) `+"`id`"+` "x" \n`))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
	assert.Equal(t, `''\'''\'''`, shellQuote("''"))
}
