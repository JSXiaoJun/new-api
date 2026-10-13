package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopUpBonusLogPartOnlyNotesPositiveBonus(t *testing.T) {
	require.Nil(t, topUpBonusLogPart(1000, 1000))
	require.Nil(t, topUpBonusLogPart(0, 0))

	part := topUpBonusLogPart(1100, 1000)
	require.NotNil(t, part)
	require.Equal(t, "Includes top-up bonus {{quota}}", part.Key)
	require.Contains(t, part.Params, "quota")
}
