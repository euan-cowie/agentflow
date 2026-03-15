package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "update CLI help golden files")

func TestCLIHelpGolden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		args   []string
		golden string
	}{
		{
			name:   "root-help",
			args:   []string{"--help"},
			golden: "root-help.golden",
		},
		{
			name:   "config-help",
			args:   []string{"config", "--help"},
			golden: "config-help.golden",
		},
		{
			name:   "auth-help",
			args:   []string{"auth", "--help"},
			golden: "auth-help.golden",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := executeCLI(t, tc.args...)
			goldenPath := filepath.Join("testdata", tc.golden)
			if *updateGolden || os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s\n\ngot:\n%s\nwant:\n%s", tc.name, got, string(want))
			}
		})
	}
}

func executeCLI(t *testing.T, args ...string) string {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute CLI %v: %v", args, err)
	}
	return out.String()
}
