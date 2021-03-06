package chunkstore_test

import (
	"github.com/nyaxt/otaru/blobstore"
	"github.com/nyaxt/otaru/btncrypt"
	"github.com/nyaxt/otaru/chunkstore"
	"github.com/nyaxt/otaru/inodedb"
	. "github.com/nyaxt/otaru/testutils"

	"bytes"
	"fmt"
	"reflect"
	"testing"
)

type SimpleDBChunksArrayIO struct {
	cs []inodedb.FileChunk
}

var _ = chunkstore.ChunksArrayIO(&SimpleDBChunksArrayIO{})

func NewSimpleDBChunksArrayIO() *SimpleDBChunksArrayIO {
	return &SimpleDBChunksArrayIO{make([]inodedb.FileChunk, 0)}
}

func (caio *SimpleDBChunksArrayIO) Read() ([]inodedb.FileChunk, error) {
	return caio.cs, nil
}

func (caio *SimpleDBChunksArrayIO) Write(cs []inodedb.FileChunk) error {
	caio.cs = cs
	return nil
}

func (caio *SimpleDBChunksArrayIO) Close() error { return nil }

func TestChunkedFileIO_FileBlobStore(t *testing.T) {
	caio := NewSimpleDBChunksArrayIO()
	fbs := TestFileBlobStore()
	cfio := chunkstore.NewChunkedFileIO(fbs, TestCipher(), caio)

	if err := cfio.PWrite(0, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}
	readtgt := make([]byte, len(HelloWorld))
	if err := cfio.PRead(0, readtgt); err != nil {
		t.Errorf("PRead failed: %v", err)
		return
	}
	if !bytes.Equal(HelloWorld, readtgt) {
		t.Errorf("read content invalid: %v", readtgt)
	}

	if int64(len(HelloWorld)) != cfio.Size() {
		t.Errorf("len invalid: %v", cfio.Size())
	}
}

func TestChunkedFileIO_SingleChunk(t *testing.T) {
	caio := NewSimpleDBChunksArrayIO()
	bs := blobstore.NewMockBlobStore()
	cfio := chunkstore.NewChunkedFileIO(bs, TestCipher(), caio)

	// Disable Chunk framing for testing
	cfio.OverrideNewChunkIOForTesting(func(bh blobstore.BlobHandle, c btncrypt.Cipher, offset int64) blobstore.BlobHandle { return bh })

	if err := cfio.PWrite(123, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}
	if err := cfio.PWrite(456, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}

	if len(caio.cs) != 1 {
		t.Errorf("len(caio.cs) %d", len(caio.cs))
		return
	}
	if caio.cs[0].Offset != 0 {
		t.Errorf("Chunk at invalid offset: %d", caio.cs[1].Offset)
	}
	bh := bs.Paths[caio.cs[0].BlobPath]
	if bh.Log[0].Offset != 123 {
		t.Errorf("Chunk write at invalid offset: %d", bh.Log[0].Offset)
	}
	if bh.Log[1].Offset != 456 {
		t.Errorf("Chunk write at invalid offset: %d", bh.Log[0].Offset)
	}
}

func TestChunkedFileIO_MultiChunk(t *testing.T) {
	caio := NewSimpleDBChunksArrayIO()
	bs := blobstore.NewMockBlobStore()
	cfio := chunkstore.NewChunkedFileIO(bs, TestCipher(), caio)

	// Disable Chunk framing for testing
	cfio.OverrideNewChunkIOForTesting(func(bh blobstore.BlobHandle, c btncrypt.Cipher, offset int64) blobstore.BlobHandle { return bh })

	if err := cfio.PWrite(chunkstore.ChunkSplitSize+12345, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}
	if err := cfio.PWrite(123, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}

	if len(caio.cs) != 2 {
		t.Errorf("len(caio.cs) %d", len(caio.cs))
		return
	}
	if caio.cs[0].Offset != 0 {
		t.Errorf("Chunk at invalid offset: %d", caio.cs[1].Offset)
	}
	bh := bs.Paths[caio.cs[0].BlobPath]
	if bh.Log[0].Offset != 123 {
		t.Errorf("Chunk write at invalid offset: %d", bh.Log[0].Offset)
	}
	if caio.cs[1].Offset != chunkstore.ChunkSplitSize {
		t.Errorf("Split chunk at invalid offset: %d", caio.cs[1].Offset)
	}
	bh = bs.Paths[caio.cs[1].BlobPath]
	if bh.Log[0].Offset != 12345 {
		t.Errorf("Split chunk write at invalid offset: %d", bh.Log[0].Offset)
	}

	if err := cfio.PWrite(chunkstore.ChunkSplitSize-5, HelloWorld); err != nil {
		t.Errorf("PWrite failed: %v", err)
		return
	}
	bh = bs.Paths[caio.cs[1].BlobPath]
	if !reflect.DeepEqual(bh.Log[1], blobstore.MockBlobStoreOperation{'W', 0, 7, HelloWorld[5]}) {
		fmt.Printf("? %+v\n", bh.Log[1])
	}
}
