package tools

import "strings"

import "testing"

func TestRestyleTextElement(t *testing.T) {
	fill := func(c string) styleEdit { return styleEdit{fill: c, setFill: true} }
	size := func(px float64) styleEdit { return styleEdit{fontSize: px, setSize: true} }

	cases := []struct {
		name       string
		svg        string
		want       string
		occ        int
		edit       styleEdit
		wantSub    []string // substrings that must be present after
		wantAbsent []string // substrings that must be gone
		wantErr    bool
	}{
		{
			name:    "replace existing fill on plain text",
			svg:     `<svg><text x="100" y="200" fill="#000000">Hello</text></svg>`,
			want:    "Hello",
			occ:     1,
			edit:    fill("#FF0000"),
			wantSub: []string{`fill="#FF0000"`, `>Hello</text>`},
		},
		{
			name:    "add fill when absent",
			svg:     `<svg><text x="100" y="200">World</text></svg>`,
			want:    "World",
			occ:     1,
			edit:    fill("#123456"),
			wantSub: []string{`fill="#123456"`, `>World</text>`},
		},
		{
			name:       "text-level fill wins over tspan fill (tspan stripped)",
			svg:        `<svg><text x="1" y="2" fill="#000000"><tspan fill="#111111">A</tspan><tspan>B</tspan></text></svg>`,
			want:       "AB",
			occ:        1,
			edit:       fill("#FF0000"),
			wantSub:    []string{`<text x="1" y="2" fill="#FF0000">`, `<tspan>A</tspan>`},
			wantAbsent: []string{`fill="#111111"`},
		},
		{
			name:    "absolute font-size",
			svg:     `<svg><text x="1" y="2" font-size="24" fill="#000000">Big</text></svg>`,
			want:    "Big",
			occ:     1,
			edit:    size(48),
			wantSub: []string{`font-size="48"`},
		},
		{
			name:    "occurrence picks the second identical text",
			svg:     `<svg><text x="1" y="1" fill="#000000">Same</text><text x="9" y="9" fill="#000000">Same</text></svg>`,
			want:    "Same",
			occ:     2,
			edit:    fill("#FF0000"),
			wantSub: []string{`<text x="9" y="9" fill="#FF0000">Same</text>`, `<text x="1" y="1" fill="#000000">Same</text>`},
		},
		{
			name:    "whitespace-normalized tspan match across newlines",
			svg:     "<svg><text x=\"1\" y=\"2\" fill=\"#000000\">\n  <tspan>第一行</tspan>\n  <tspan>第二行</tspan>\n</text></svg>",
			want:    "第一行 第二行",
			occ:     1,
			edit:    fill("#FF0000"),
			wantSub: []string{`fill="#FF0000"`},
		},
		{
			name:    "ignores textPath false-positive",
			svg:     `<svg><textPath href="#p">decoy</textPath><text x="1" y="2">Real</text></svg>`,
			want:    "Real",
			occ:     1,
			edit:    fill("#FF0000"),
			wantSub: []string{`<text x="1" y="2" fill="#FF0000">Real</text>`},
		},
		{
			name:    "no match errors",
			svg:     `<svg><text x="1" y="2">Present</text></svg>`,
			want:    "Absent",
			occ:     1,
			edit:    fill("#FF0000"),
			wantErr: true,
		},
		{
			name:    "occurrence out of range errors",
			svg:     `<svg><text x="1" y="2">Only</text></svg>`,
			want:    "Only",
			occ:     2,
			edit:    fill("#FF0000"),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := restyleTextElement(tc.svg, tc.want, tc.occ, tc.edit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (out=%q)", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(out, sub) {
					t.Errorf("missing %q in:\n%s", sub, out)
				}
			}
			for _, sub := range tc.wantAbsent {
				if strings.Contains(out, sub) {
					t.Errorf("unexpected %q in:\n%s", sub, out)
				}
			}
		})
	}
}
