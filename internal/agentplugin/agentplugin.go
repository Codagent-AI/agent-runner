package agentplugin

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

const Source = "Codagent-AI/agent-skills"

var ErrBinaryMissing = errors.New("agent-plugin binary not found on PATH")

var lookPath = exec.LookPath

var runCommand = func(binary string, args []string) (string, error) {
	cmd := exec.Command(binary, args...) // #nosec G204 -- binary resolved from LookPath
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	err := cmd.Run()
	return stdout.String(), err
}

type Request struct {
	CLIs  []string
	Scope string
}

type Plan struct {
	Binary  string
	CLIs    []string
	Project bool
}

type Preview struct {
	Output string
}

type Result struct {
	Output  string
	Warning string
}

func Resolve(req *Request) (*Plan, error) {
	if len(req.CLIs) == 0 {
		return nil, nil
	}
	binary, err := lookPath("agent-plugin")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBinaryMissing, err)
	}
	return &Plan{
		Binary:  binary,
		CLIs:    req.CLIs,
		Project: req.Scope == "project",
	}, nil
}

func DryRun(plan *Plan) (*Preview, error) {
	args := buildArgs(plan, "--dry-run")
	output, err := runCommand(plan.Binary, args)
	if err != nil {
		return nil, fmt.Errorf("agent-plugin dry-run: %w", err)
	}
	return &Preview{Output: output}, nil
}

func Install(plan *Plan) (*Result, error) {
	args := buildArgs(plan, "--yes")
	output, err := runCommand(plan.Binary, args)
	if err != nil {
		return &Result{
			Output:  output,
			Warning: err.Error(),
		}, nil
	}
	return &Result{Output: output}, nil
}

func buildArgs(plan *Plan, extra string) []string {
	args := []string{"add", Source}
	for _, cli := range plan.CLIs {
		args = append(args, "--agent", cli)
	}
	if plan.Project {
		args = append(args, "--project")
	}
	args = append(args, extra)
	return args
}
