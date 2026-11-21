package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheck(t *testing.T) {
	tags := []string{"v0.1.0", "v0.2.0", "v0.2.1-rc.1", "not-a-version"}
	cases := []struct {
		version string
		tagged  bool
		err     string
	}{
		{"0.2.1", false, ""},
		{"0.3.0", false, ""},
		{"0.2.1-rc.2", false, ""},
		{"0.2.0", true, ""},
		{"0.2.1-rc.1", true, ""},
		{"0.1.5", false, "not greater than the latest tag v0.2.1-rc.1"},
		{"0.2.1-rc.0", false, "not greater"},
		{"0.2.1-beta", false, "not greater"},
		{"v0.2.1", false, "not a semantic version"},
		{"0.2", false, "not a semantic version"},
		{"1", false, "not a semantic version"},
		{"", false, "not a semantic version"},
	}
	for _, c := range cases {
		tagged, err := check(c.version, tags)
		if c.err != "" {
			assert.ErrorContains(t, err, c.err, c.version)
			continue
		}
		assert.NoError(t, err, c.version)
		assert.Equal(t, c.tagged, tagged, c.version)
	}
	assert.True(t, isPrerelease("0.2.1-rc.1"))
	assert.True(t, isPrerelease("1.0.0-beta+build.5"))
	assert.False(t, isPrerelease("0.2.1"))
	assert.False(t, isPrerelease("1.0.0+build.5"))

	published := []string{"v0.1.0", "v0.2.0", "v0.2.2", "v0.3.0-rc.1", "junk"}
	assert.False(t, isLatest("0.2.1", published), "older than v0.2.2")
	assert.True(t, isLatest("0.2.2", published), "equal to the highest")
	assert.True(t, isLatest("0.2.3", published))
	assert.False(t, isLatest("0.3.0-rc.2", published), "prereleases are never latest")
	assert.True(t, isLatest("0.1.0", nil), "first release")

	tagged, err := check("0.1.0", nil)
	assert.NoError(t, err)
	assert.False(t, tagged)
}
