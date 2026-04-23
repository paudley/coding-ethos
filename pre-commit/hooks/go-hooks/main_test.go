package main

import "testing"

func TestLimitsForFilePreservesPythonLimitsUnderScripts(t *testing.T) {
	cfg := Config{}
	cfg.LineLimits.PythonHard = 1000
	cfg.LineLimits.PythonWarn = 800
	cfg.LineLimits.ShellHard = 500
	cfg.LineLimits.ShellWarn = 400

	tests := []struct {
		path     string
		hardWant int
		warnWant int
	}{
		{
			path:     "scripts/tool.py",
			hardWant: 1000,
			warnWant: 800,
		},
		{
			path:     "scripts/tool.sh",
			hardWant: 500,
			warnWant: 400,
		},
		{
			path:     "coding_ethos/module.py",
			hardWant: 1000,
			warnWant: 800,
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			hardGot, warnGot := limitsForFile(cfg, tc.path)
			if hardGot != tc.hardWant || warnGot != tc.warnWant {
				t.Fatalf(
					"limitsForFile(%q) = (%d, %d), want (%d, %d)",
					tc.path,
					hardGot,
					warnGot,
					tc.hardWant,
					tc.warnWant,
				)
			}
		})
	}
}
