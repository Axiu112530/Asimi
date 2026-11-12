//go:build !windows

package command

import "context"

func directoryIdentity(string) (string, error) { return "", Error("COMMAND_UNSUPPORTED_PLATFORM") }

func fixedDrive(root string) bool { return false }

func runManaged(ctx context.Context, p Proposal, targets []projectTarget) (Result, error) {
	return Result{ExitCode: -1}, Error("COMMAND_UNSUPPORTED_PLATFORM")
}
