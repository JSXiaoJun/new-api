package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUsersForPaginationTest(t *testing.T, total int) {
	t.Helper()
	for id := 1; id <= total; id++ {
		user := &User{
			Id:          id,
			Username:    fmt.Sprintf("user%02d", id),
			Password:    "password123",
			DisplayName: fmt.Sprintf("User %02d", id),
			Email:       fmt.Sprintf("user%02d@example.com", id),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AffCode:     fmt.Sprintf("aff%02d", id),
		}
		require.NoError(t, DB.Create(user).Error)
	}
}

func collectUserIDs(users []*User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	return ids
}

func TestGetAllUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	pageOne, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, collectUserIDs(pageOne))

	pageTwo, total, err := GetAllUsers(&common.PageInfo{Page: 2, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(pageTwo))

	pageThree, total, err := GetAllUsers(&common.PageInfo{Page: 3, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{41, 42}, collectUserIDs(pageThree))
}

func TestSearchUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	users, total, err := SearchUsers("user", "", nil, nil, 0, nil, 20, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(users))
}

func TestSearchUsersMatchesApiKey(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 3)

	require.NoError(t, DB.Create([]Token{
		{
			UserId:       2,
			Key:          "user02-searchable",
			Name:         "search token",
			Status:       common.TokenStatusEnabled,
			CreatedTime:  1,
			AccessedTime: 1,
			ExpiredTime:  -1,
		},
		{
			UserId:       3,
			Key:          "prefix-user02-searchable-suffix",
			Name:         "partial-match token",
			Status:       common.TokenStatusEnabled,
			CreatedTime:  1,
			AccessedTime: 1,
			ExpiredTime:  -1,
		},
	}).Error)

	users, total, err := SearchUsers("sk-user02-searchable", "", nil, nil, 0, nil, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, []int{2}, collectUserIDs(users))
}

func TestSearchUsersFiltersByInviter(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 4)
	require.NoError(t, DB.Model(&User{}).Where("id IN ?", []int{2, 3}).Update("inviter_id", 1).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 4).Update("inviter_id", 2).Error)

	users, total, err := SearchUsers("", "", nil, nil, 1, nil, 0, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Equal(t, []int{2, 3}, collectUserIDs(users))

	users, total, err = SearchUsers("user03", "", nil, nil, 1, nil, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, []int{3}, collectUserIDs(users))

	users, total, err = SearchUsers("", "", nil, nil, 4, nil, 0, 20)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, users)
}

func TestSearchUsersFiltersByMinQuota(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 4)
	for id, quota := range map[int]int{1: -100, 2: 0, 3: 5000, 4: 5001} {
		require.NoError(t, DB.Model(&User{}).Where("id = ?", id).Update("quota", quota).Error)
	}

	tests := []struct {
		name     string
		keyword  string
		minQuota int
		wantIDs  []int
	}{
		{name: "threshold is inclusive", minQuota: 5000, wantIDs: []int{3, 4}},
		{name: "zero keeps users without quota", minQuota: 0, wantIDs: []int{2, 3, 4}},
		{name: "negative threshold keeps users in debt", minQuota: -100, wantIDs: []int{1, 2, 3, 4}},
		{name: "combines with keyword", keyword: "user04", minQuota: 5000, wantIDs: []int{4}},
		{name: "above every balance", minQuota: 5002, wantIDs: []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, total, err := SearchUsers(tt.keyword, "", nil, nil, 0, &tt.minQuota, 0, 20, NewUserSortOptions("id", "asc"))
			require.NoError(t, err)
			assert.Equal(t, int64(len(tt.wantIDs)), total)
			assert.Equal(t, tt.wantIDs, collectUserIDs(users))
		})
	}
}
