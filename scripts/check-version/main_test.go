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
	tagged, err := check("0.1.0", nil)
	assert.NoError(t, err)
	assert.False(t, tagged)
}
