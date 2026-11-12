//go:build !windows

package computer

import "context"

type unavailableDriver struct{}

func NewDriver() Driver                   { return unavailableDriver{} }
func (unavailableDriver) Available() bool { return false }
func (unavailableDriver) Enumerate(context.Context) ([]Window, error) {
	return nil, Error("CONTROL_UNAVAILABLE")
}
func (unavailableDriver) Inspect(context.Context, Window) (Observation, error) {
	return Observation{}, Error("CONTROL_UNAVAILABLE")
}
func (unavailableDriver) Execute(context.Context, Proposal) (Outcome, error) {
	return Outcome{Effect: "NOT_STARTED"}, Error("CONTROL_UNAVAILABLE")
}
func (unavailableDriver) Verify(context.Context, Proposal, Outcome) (Verification, error) {
	return Verification{Code: "CONTROL_UNAVAILABLE"}, Error("CONTROL_UNAVAILABLE")
}
func (unavailableDriver) Close() error { return nil }
