package otaru

import (
	"fmt"
	"log"
	"sync"
	"syscall"

	"github.com/nyaxt/otaru/blobstore"
	"github.com/nyaxt/otaru/btncrypt"
	"github.com/nyaxt/otaru/chunkstore"
	fl "github.com/nyaxt/otaru/flags"
	"github.com/nyaxt/otaru/inodedb"
	"github.com/nyaxt/otaru/util"
)

const (
	EACCES    = syscall.Errno(syscall.EACCES)
	EBADF     = syscall.Errno(syscall.EBADF)
	EEXIST    = syscall.Errno(syscall.EEXIST)
	EISDIR    = syscall.Errno(syscall.EISDIR)
	ENOENT    = syscall.Errno(syscall.ENOENT)
	ENOTDIR   = syscall.Errno(syscall.ENOTDIR)
	ENOTEMPTY = syscall.Errno(syscall.ENOTEMPTY)
	EPERM     = syscall.Errno(syscall.EPERM)
)

const (
	FileWriteCacheMaxPatches         = 32
	FileWriteCacheMaxPatchContentLen = 256 * 1024
)

type FileSystem struct {
	idb inodedb.DBHandler

	bs blobstore.RandomAccessBlobStore
	c  btncrypt.Cipher

	newChunkedFileIO func(bs blobstore.RandomAccessBlobStore, c btncrypt.Cipher, caio chunkstore.ChunksArrayIO) blobstore.BlobHandle

	muOpenFiles sync.Mutex
	openFiles   map[inodedb.ID]*OpenFile

	muOrigPath sync.Mutex
	origpath   map[inodedb.ID]string
}

func NewFileSystem(idb inodedb.DBHandler, bs blobstore.RandomAccessBlobStore, c btncrypt.Cipher) *FileSystem {
	fs := &FileSystem{
		idb: idb,
		bs:  bs,
		c:   c,

		newChunkedFileIO: func(bs blobstore.RandomAccessBlobStore, c btncrypt.Cipher, caio chunkstore.ChunksArrayIO) blobstore.BlobHandle {
			return chunkstore.NewChunkedFileIO(bs, c, caio)
		},

		openFiles: make(map[inodedb.ID]*OpenFile),
		origpath:  make(map[inodedb.ID]string),
	}
	fs.setOrigPathForId(inodedb.RootDirID, "/")

	return fs
}

func (fs *FileSystem) tryGetOrigPath(id inodedb.ID) string {
	fs.muOrigPath.Lock()
	defer fs.muOrigPath.Unlock()

	origpath, ok := fs.origpath[id]
	if !ok {
		log.Printf("Failed to lookup orig path for ID %d", id)
		return "<unknown>"
	}
	log.Printf("Orig path for ID %d is \"%s\"", id, origpath)
	return origpath
}

func (fs *FileSystem) setOrigPathForId(id inodedb.ID, origpath string) {
	fs.muOrigPath.Lock()
	defer fs.muOrigPath.Unlock()

	if len(origpath) == 0 {
		delete(fs.origpath, id)
	}
	fs.origpath[id] = origpath
}

func (fs *FileSystem) Sync() error {
	es := []error{}

	if s, ok := fs.idb.(util.Syncer); ok {
		if err := s.Sync(); err != nil {
			es = append(es, fmt.Errorf("Failed to sync INodeDB: %v", err))
		}
	}
	// FIXME: sync active handles

	return util.ToErrors(es)
}

func (fs *FileSystem) OverrideNewChunkedFileIOForTesting(newChunkedFileIO func(blobstore.RandomAccessBlobStore, btncrypt.Cipher, chunkstore.ChunksArrayIO) blobstore.BlobHandle) {
	fs.newChunkedFileIO = newChunkedFileIO
}

func (fs *FileSystem) DirEntries(id inodedb.ID) (map[string]inodedb.ID, error) {
	v, _, err := fs.idb.QueryNode(id, false)
	if err != nil {
		return nil, err
	}
	if v.GetType() != inodedb.DirNodeT {
		return nil, ENOTDIR
	}

	dv := v.(*inodedb.DirNodeView)

	dirorigpath := fs.tryGetOrigPath(id)
	for name, id := range dv.Entries {
		fs.setOrigPathForId(id, fmt.Sprintf("%s/%s", dirorigpath, name))
	}

	return dv.Entries, err
}

func (fs *FileSystem) Rename(srcDirID inodedb.ID, srcName string, dstDirID inodedb.ID, dstName string) error {
	tx := inodedb.DBTransaction{Ops: []inodedb.DBOperation{
		&inodedb.RenameOp{
			SrcDirID: srcDirID, SrcName: srcName,
			DstDirID: dstDirID, DstName: dstName,
		},
	}}
	if _, err := fs.idb.ApplyTransaction(tx); err != nil {
		return err
	}

	// FIXME: fs.setOrigPathForId

	return nil
}

func (fs *FileSystem) Remove(dirID inodedb.ID, name string) error {
	tx := inodedb.DBTransaction{Ops: []inodedb.DBOperation{
		&inodedb.RemoveOp{
			NodeLock: inodedb.NodeLock{dirID, inodedb.NoTicket}, Name: name,
		},
	}}
	if _, err := fs.idb.ApplyTransaction(tx); err != nil {
		return err
	}

	// FIXME: fs.setOrigPathForId

	return nil
}

func (fs *FileSystem) createNode(dirID inodedb.ID, name string, typ inodedb.Type) (inodedb.ID, error) {
	nlock, err := fs.idb.LockNode(inodedb.AllocateNewNodeID)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := fs.idb.UnlockNode(nlock); err != nil {
			log.Printf("Failed to unlock node when creating file: %v", err)
		}
	}()

	dirorigpath := fs.tryGetOrigPath(dirID)
	origpath := fmt.Sprintf("%s/%s", dirorigpath, name)

	tx := inodedb.DBTransaction{Ops: []inodedb.DBOperation{
		&inodedb.CreateNodeOp{NodeLock: nlock, OrigPath: origpath, Type: typ},
		&inodedb.HardLinkOp{NodeLock: inodedb.NodeLock{dirID, inodedb.NoTicket}, Name: name, TargetID: nlock.ID},
	}}
	if _, err := fs.idb.ApplyTransaction(tx); err != nil {
		return 0, err
	}

	fs.setOrigPathForId(nlock.ID, origpath)

	return nlock.ID, nil
}

func (fs *FileSystem) CreateFile(dirID inodedb.ID, name string) (inodedb.ID, error) {
	return fs.createNode(dirID, name, inodedb.FileNodeT)
}

func (fs *FileSystem) CreateDir(dirID inodedb.ID, name string) (inodedb.ID, error) {
	return fs.createNode(dirID, name, inodedb.DirNodeT)
}

type Attr struct {
	ID   inodedb.ID
	Type inodedb.Type
	Size int64
}

func (fs *FileSystem) Attr(id inodedb.ID) (Attr, error) {
	v, _, err := fs.idb.QueryNode(id, false)
	if err != nil {
		return Attr{}, err
	}

	size := int64(0)
	if fn, ok := v.(*inodedb.FileNodeView); ok {
		size = fn.Size
	}

	a := Attr{
		ID:   v.GetID(),
		Type: v.GetType(),
		Size: size,
	}
	return a, nil
}

func (fs *FileSystem) IsDir(id inodedb.ID) (bool, error) {
	v, _, err := fs.idb.QueryNode(id, false)
	if err != nil {
		return false, err
	}

	return v.GetType() == inodedb.DirNodeT, nil
}

type FileHandle struct {
	of    *OpenFile
	flags int
}

type OpenFile struct {
	fs    *FileSystem
	nlock inodedb.NodeLock
	wc    *FileWriteCache
	cfio  blobstore.BlobHandle

	origFilename string

	handles []*FileHandle

	mu sync.Mutex
}

func (fs *FileSystem) getOrCreateOpenFile(id inodedb.ID) *OpenFile {
	fs.muOpenFiles.Lock()
	defer fs.muOpenFiles.Unlock()

	of, ok := fs.openFiles[id]
	if ok {
		return of
	}
	of = &OpenFile{
		fs: fs,
		wc: NewFileWriteCache(),

		handles: make([]*FileHandle, 0, 1),
	}
	fs.openFiles[id] = of
	return of
}

type origFilenameSetter interface {
	SetOrigFilename(name string)
}

func (fs *FileSystem) OpenFile(id inodedb.ID, flags int) (*FileHandle, error) {
	log.Printf("OpenFile(id: %v, flags rok: %t wok: %t)", id, fl.IsReadAllowed(flags), fl.IsWriteAllowed(flags))

	tryLock := fl.IsWriteAllowed(flags)
	if tryLock && !fl.IsWriteAllowed(fs.bs.Flags()) {
		return nil, EACCES
	}

	of := fs.getOrCreateOpenFile(id)

	of.mu.Lock()
	defer of.mu.Unlock()

	ofIsInitialized := of.nlock.ID != 0
	if ofIsInitialized && (of.nlock.HasTicket() || !tryLock) {
		// No need to upgrade lock. Just use cached filehandle.
		log.Printf("Using cached of for inode id: %v", id)
		return of.OpenHandleWithoutLock(flags), nil
	}

	// upgrade lock or acquire new lock...
	v, nlock, err := fs.idb.QueryNode(id, tryLock)
	if err != nil {
		return nil, err
	}
	if v.GetType() != inodedb.FileNodeT {
		if err := fs.idb.UnlockNode(nlock); err != nil {
			log.Printf("Unlock node failed for non-file node: %v", err)
		}

		if v.GetType() == inodedb.DirNodeT {
			return nil, EISDIR
		}
		return nil, fmt.Errorf("Specified node not file but has type %v", v.GetType())
	}

	of.nlock = nlock
	caio := NewINodeDBChunksArrayIO(fs.idb, nlock)
	of.cfio = fs.newChunkedFileIO(fs.bs, fs.c, caio)
	if setter, ok := of.cfio.(origFilenameSetter); ok {
		setter.SetOrigFilename(fs.tryGetOrigPath(nlock.ID))
	}
	return of.OpenHandleWithoutLock(flags), nil
}

func (of *OpenFile) OpenHandleWithoutLock(flags int) *FileHandle {
	fh := &FileHandle{of: of, flags: flags}
	of.handles = append(of.handles, fh)
	return fh
}

func (of *OpenFile) CloseHandle(tgt *FileHandle) {
	if tgt.of == nil {
		log.Printf("Detected FileHandle double close!")
		return
	}
	if tgt.of != of {
		log.Fatalf("Attempt to close handle for other OpenFile. tgt fh: %+v, of: %+v", tgt, of)
		return
	}

	wasWriteHandle := fl.IsWriteAllowed(tgt.flags)
	ofHasOtherWriteHandle := false

	tgt.of = nil

	of.mu.Lock()
	defer of.mu.Unlock()

	// remove tgt from of.handles slice
	newHandles := make([]*FileHandle, 0, len(of.handles)-1)
	for _, h := range of.handles {
		if h != tgt {
			if fl.IsWriteAllowed(h.flags) {
				ofHasOtherWriteHandle = true
			}
			newHandles = append(newHandles, h)
		}
	}
	of.handles = newHandles

	if wasWriteHandle && !ofHasOtherWriteHandle {
		of.downgradeToReadLock()
	}
}

func (of *OpenFile) downgradeToReadLock() {
	log.Printf("Downgrade %v to read lock.", of)
	// Note: assumes of.mu is Lock()-ed

	if !of.nlock.HasTicket() {
		log.Printf("Attempt to downgrade node lock, but no excl lock found. of: %v", of)
		return
	}

	if err := of.fs.idb.UnlockNode(of.nlock); err != nil {
		log.Printf("Unlocking node to downgrade to read lock failed: %v", err)
	}
	of.nlock.Ticket = inodedb.NoTicket
	caio := NewINodeDBChunksArrayIO(of.fs.idb, of.nlock)
	of.cfio = of.fs.newChunkedFileIO(of.fs.bs, of.fs.c, caio)
}

func (of *OpenFile) updateSizeWithoutLock(newsize int64) error {
	tx := inodedb.DBTransaction{Ops: []inodedb.DBOperation{
		&inodedb.UpdateSizeOp{NodeLock: of.nlock, Size: newsize},
	}}
	if _, err := of.fs.idb.ApplyTransaction(tx); err != nil {
		return fmt.Errorf("Failed to update FileNode size: %v", err)
	}
	return nil
}

// sizeMayFailWithoutLock returns file size if succeed. The size query may fail with an error.
func (of *OpenFile) sizeMayFailWithoutLock() (int64, error) {
	v, _, err := of.fs.idb.QueryNode(of.nlock.ID, false)
	if err != nil {
		return 0, fmt.Errorf("Failed to QueryNode inodedb: %v", err)
	}
	fv, ok := v.(*inodedb.FileNodeView)
	if !ok {
		return 0, fmt.Errorf("Non-FileNodeView returned from QueryNode. Type: %v", v.GetType())
	}
	return fv.Size, nil
}

func (of *OpenFile) PWrite(offset int64, p []byte) error {
	of.mu.Lock()
	defer of.mu.Unlock()

	currentSize, err := of.sizeMayFailWithoutLock()
	if err != nil {
		return err
	}

	// Pass wc.PWrite a copy of "p", as wc.PWrite expects its slice to be never modified afterwards.
	pcopy := make([]byte, len(p))
	copy(pcopy, p)
	if err := of.wc.PWrite(offset, pcopy); err != nil {
		return err
	}

	if of.wc.NeedsSync() {
		if err := of.wc.Sync(of.cfio); err != nil {
			return err
		}
	}

	right := offset + int64(len(p))
	if right > currentSize {
		return of.updateSizeWithoutLock(right)
	}

	return nil
}

func (of *OpenFile) PRead(offset int64, p []byte) error {
	of.mu.Lock()
	defer of.mu.Unlock()

	return of.wc.PReadThrough(offset, p, of.cfio)
}

func (of *OpenFile) Sync() error {
	of.mu.Lock()
	defer of.mu.Unlock()

	if err := of.wc.Sync(of.cfio); err != nil {
		return fmt.Errorf("FileWriteCache sync failed: %v", err)
	}
	return nil
}

func (of *OpenFile) Size() int64 {
	of.mu.Lock()
	defer of.mu.Unlock()

	size, err := of.sizeMayFailWithoutLock()
	if err != nil {
		return 0
	}
	return size
}

func (of *OpenFile) Truncate(newsize int64) error {
	of.mu.Lock()
	defer of.mu.Unlock()

	oldsize, err := of.sizeMayFailWithoutLock()
	if err != nil {
		return err
	}

	if newsize > oldsize {
		return of.updateSizeWithoutLock(newsize)
	} else if newsize < oldsize {
		of.wc.Truncate(newsize)
		of.cfio.Truncate(newsize)
		return of.updateSizeWithoutLock(newsize)
	} else {
		return nil
	}
}

func (fh *FileHandle) ID() inodedb.ID {
	return fh.of.nlock.ID
}

func (fh *FileHandle) PWrite(offset int64, p []byte) error {
	if !fl.IsWriteAllowed(fh.flags) {
		return EBADF
	}

	return fh.of.PWrite(offset, p)
}

func (fh *FileHandle) PRead(offset int64, p []byte) error {
	if !fl.IsReadAllowed(fh.flags) {
		return EBADF
	}

	return fh.of.PRead(offset, p)
}

func (fh *FileHandle) Sync() error {
	if !fl.IsWriteAllowed(fh.flags) {
		return nil
	}

	return fh.of.Sync()
}

func (fh *FileHandle) Size() int64 {
	return fh.of.Size()
}

func (fh *FileHandle) Truncate(newsize int64) error {
	if !fl.IsWriteAllowed(fh.flags) {
		return EBADF
	}

	return fh.of.Truncate(newsize)
}

func (fh *FileHandle) Close() {
	fh.of.CloseHandle(fh)
}
