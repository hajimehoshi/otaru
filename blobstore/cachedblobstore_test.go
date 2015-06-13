package blobstore_test

import (
	"testing"

	"github.com/nyaxt/otaru/blobstore"
	"github.com/nyaxt/otaru/flags"
	tu "github.com/nyaxt/otaru/testutils"
)

func TestCachedBlobStore(t *testing.T) {
	backendbs := tu.TestFileBlobStoreOfName("backend")
	cachebs := tu.TestFileBlobStoreOfName("cache")

	if err := tu.WriteVersionedBlob(backendbs, "backendonly", 5); err != nil {
		t.Errorf("%v", err)
		return
	}

	bs, err := blobstore.NewCachedBlobStore(backendbs, cachebs, flags.O_RDWRCREATE, tu.TestQueryVersion)
	if err != nil {
		t.Errorf("Failed to create CachedBlobStore: %v", err)
		return
	}
	if err := tu.AssertBlobVersion(backendbs, "backendonly", 5); err != nil {
		t.Errorf("%v", err)
		return
	}
	// assert cache not yet filled
	if err := tu.AssertBlobVersion(cachebs, "backendonly", 0); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersionRA(bs, "backendonly", 5); err != nil {
		t.Errorf("%v", err)
		return
	}

	// assert cache fill
	if err := tu.AssertBlobVersion(cachebs, "backendonly", 5); err != nil {
		t.Errorf("%v", err)
		return
	}

	if err := tu.WriteVersionedBlobRA(bs, "backendonly", 10); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := bs.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
		return
	}

	if err := tu.AssertBlobVersionRA(bs, "backendonly", 10); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersion(cachebs, "backendonly", 10); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersion(backendbs, "backendonly", 10); err != nil {
		t.Errorf("%v", err)
		return
	}
}

func TestCachedBlobStore_Invalidate(t *testing.T) {
	backendbs := tu.TestFileBlobStoreOfName("backend")
	cachebs := tu.TestFileBlobStoreOfName("cache")

	if err := tu.WriteVersionedBlob(cachebs, "backendnewer", 2); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.WriteVersionedBlob(backendbs, "backendnewer", 3); err != nil {
		t.Errorf("%v", err)
		return
	}

	bs, err := blobstore.NewCachedBlobStore(backendbs, cachebs, flags.O_RDWRCREATE, tu.TestQueryVersion)
	if err != nil {
		t.Errorf("Failed to create CachedBlobStore: %v", err)
		return
	}
	if err := tu.AssertBlobVersionRA(bs, "backendnewer", 3); err != nil {
		t.Errorf("%v", err)
		return
	}

	// assert cache fill
	if err := tu.AssertBlobVersion(cachebs, "backendnewer", 3); err != nil {
		t.Errorf("%v", err)
		return
	}

	if err := tu.WriteVersionedBlobRA(bs, "backendnewer", 4); err != nil {
		t.Errorf("%v", err)
		return
	}

	if err := tu.AssertBlobVersionRA(bs, "backendnewer", 4); err != nil {
		t.Errorf("%v", err)
		return
	}

	if err := bs.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
		return
	}
	if err := tu.AssertBlobVersion(cachebs, "backendnewer", 4); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersion(backendbs, "backendnewer", 4); err != nil {
		t.Errorf("%v", err)
		return
	}
}

func TestCachedBlobStore_NewEntry(t *testing.T) {
	backendbs := tu.TestFileBlobStoreOfName("backend")
	cachebs := tu.TestFileBlobStoreOfName("cache")

	bs, err := blobstore.NewCachedBlobStore(backendbs, cachebs, flags.O_RDWRCREATE, tu.TestQueryVersion)
	if err != nil {
		t.Errorf("Failed to create CachedBlobStore: %v", err)
		return
	}

	if err := tu.WriteVersionedBlobRA(bs, "newentry", 1); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersionRA(bs, "newentry", 1); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := bs.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
		return
	}
	if err := tu.AssertBlobVersion(cachebs, "newentry", 1); err != nil {
		t.Errorf("%v", err)
		return
	}
	if err := tu.AssertBlobVersion(backendbs, "newentry", 1); err != nil {
		t.Errorf("%v", err)
		return
	}
}
