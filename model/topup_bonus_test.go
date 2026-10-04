package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopUpBonusLogSuffixOnlyNotesPositiveBonus(t *testing.T) {
	require.Empty(t, topUpBonusLogSuffix(1000, 1000))
	require.Empty(t, topUpBonusLogSuffix(0, 0))

	suffix := topUpBonusLogSuffix(1100, 1000)
	require.True(t, strings.HasPrefix(suffix, "（含充值赠送 "), suffix)
	require.True(t, strings.HasSuffix(suffix, "）"), suffix)
}
