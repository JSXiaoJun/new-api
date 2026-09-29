package types

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestHideErrMsgKeepsCauseButHidesMessage(t *testing.T) {
	origin := fmt.Errorf("Post \"https://upstream.example/v1?key=secret\": %w", context.Canceled)

	apiErr := NewError(origin, ErrorCodeDoRequestFailed, ErrOptionWithHideErrMsg("upstream error: do request failed"))

	if got := apiErr.Error(); got != "upstream error: do request failed" {
		t.Fatalf("message must be replaced, got %q", got)
	}
	if !errors.Is(apiErr, context.Canceled) {
		t.Fatal("context.Canceled must stay reachable through the hidden error")
	}
}
