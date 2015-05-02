package otaru_test

import (
	. "github.com/nyaxt/otaru"
	. "github.com/nyaxt/otaru/testutils"

	"bytes"
	"os"
	"testing"
)

func TestFileWriteRead(t *testing.T) {
	bs := TestFileBlobStore()
	fs := NewFileSystemEmpty(bs, TestCipher())
	h, err := fs.OpenFileFullPath("/hello.txt", os.O_CREATE, 0666)
	if err != nil {
		t.Errorf("CreateFile failed: %v", err)
		return
	}

	err = h.PWrite(0, []byte("hello world!\n"))
	if err != nil {
		t.Errorf("PWrite failed")
	}

	buf := make([]byte, 13)
	err = h.PRead(0, buf)
	if err != nil {
		t.Errorf("PRead failed")
	}
	if !bytes.Equal([]byte("hello world!\n"), buf) {
		t.Errorf("PRead content != PWrite content")
	}
}
