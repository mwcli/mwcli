package cmdutil

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteTable(t *testing.T) {
	var b bytes.Buffer
	assert.NoError(t, WriteTable(&b, [][]string{
		{"1", "제타위키봇", "", "첫 편집"},
		{"22", "Alice", "m", "x"},
	}))
	// 제타위키봇 is 10 cells wide; an empty last cell leaves no trailing spaces.
	assert.Equal(t, "1   제타위키봇     첫 편집\n22  Alice       m  x\n", b.String())
	assert.Equal(t, "가나…", Truncate("가나다라", 5))
	assert.Equal(t, "abc", Truncate("abc", 5))
}
