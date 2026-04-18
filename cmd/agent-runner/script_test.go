package main

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"agent-runner": func() { main() },
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Setup: func(env *testscript.Env) error {
			env.Setenv("HOME", env.WorkDir)
			env.Setenv("AGENT_RUNNER_NO_TUI", "1")
			return nil
		},
	})
}
