package services

import (
	"context"
	"hugo-cms/pkg/config"
	"os/exec"
	"strings"
)

func generatorCommand(runtime config.SiteRuntime, name string, args ...string) *exec.Cmd {
	return generatorCommandWithEnv(runtime, nil, name, args...)
}

func generatorCommandWithEnv(runtime config.SiteRuntime, extraEnv []string, name string, args ...string) *exec.Cmd {
	cmdName, cmdArgs := generatorCommandSpec(runtime, name, args...)
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Dir = runtime.RepoPath
	cmd.Env = generatorProcessEnvironment(extraEnv...)
	return cmd
}

func generatorCommandContext(ctx context.Context, runtime config.SiteRuntime, name string, args ...string) *exec.Cmd {
	return generatorCommandContextWithEnv(ctx, runtime, nil, name, args...)
}

func generatorCommandContextWithEnv(ctx context.Context, runtime config.SiteRuntime, extraEnv []string, name string, args ...string) *exec.Cmd {
	cmdName, cmdArgs := generatorCommandSpec(runtime, name, args...)
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Dir = runtime.RepoPath
	cmd.Env = generatorProcessEnvironment(extraEnv...)
	return cmd
}

func generatorCommandSpec(runtime config.SiteRuntime, name string, args ...string) (string, []string) {
	if useMiseRuntime(runtime) {
		miseArgs := []string{"exec", "-C", runtime.RepoPath, "--", name}
		miseArgs = append(miseArgs, args...)
		return "mise", miseArgs
	}
	return name, args
}

func useMiseRuntime(runtime config.SiteRuntime) bool {
	return strings.EqualFold(strings.TrimSpace(runtime.Runtime), "mise")
}
