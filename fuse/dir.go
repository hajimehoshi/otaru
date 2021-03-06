package fuse

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nyaxt/otaru"
	"github.com/nyaxt/otaru/inodedb"

	bfuse "bazil.org/fuse"
	bfs "bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

type DirNode struct {
	fs *otaru.FileSystem
	id inodedb.ID
}

func (d DirNode) Attr(ctx context.Context, a *bfuse.Attr) error {
	log.Printf("DirNode Attr id: %d", d.id)

	attr, err := d.fs.Attr(d.id)
	if err != nil {
		panic("bfs.Attr failed for DirNode")
	}

	a.Inode = uint64(d.id)
	a.Mode = os.ModeDir | 0777
	a.Atime = time.Now()
	a.Mtime = time.Now()
	a.Ctime = time.Now()
	a.Crtime = time.Now()
	a.Size = uint64(attr.Size)
	return nil
}

func (d DirNode) Lookup(ctx context.Context, name string) (bfs.Node, error) {
	entries, err := d.fs.DirEntries(d.id)
	if err != nil {
		return nil, err
	}

	if id, ok := entries[name]; ok {
		isdir, err := d.fs.IsDir(id)
		if err != nil {
			log.Fatalf("Stale inode in dir? Failed IsDir: %v", err)
		}
		if isdir {
			return DirNode{d.fs, id}, nil
		} else {
			return FileNode{d.fs, id}, nil
		}
	}

	return nil, bfuse.ENOENT
}

func (d DirNode) Create(ctx context.Context, req *bfuse.CreateRequest, resp *bfuse.CreateResponse) (bfs.Node, bfs.Handle, error) {
	id, err := d.fs.CreateFile(d.id, req.Name) // req.Flags req.Mode
	if err != nil {
		return nil, nil, err
	}

	h, err := d.fs.OpenFile(id, Bazil2OtaruFlags(req.Flags))
	if err != nil {
		return nil, nil, err
	}

	return FileNode{d.fs, id}, FileHandle{h}, nil
}

func (d DirNode) ReadDirAll(ctx context.Context) ([]bfuse.Dirent, error) {
	entries, err := d.fs.DirEntries(d.id)
	if err != nil {
		return nil, err
	}

	fentries := make([]bfuse.Dirent, 0, len(entries))
	for name, id := range entries {
		t := bfuse.DT_File // FIXME!!!

		fentries = append(fentries, bfuse.Dirent{
			Inode: uint64(id),
			Name:  name,
			Type:  t,
		})
	}
	return fentries, nil
}

func (d DirNode) Rename(ctx context.Context, req *bfuse.RenameRequest, newDir bfs.Node) error {
	newdn, ok := newDir.(DirNode)
	if !ok {
		return fmt.Errorf("Node for provided target dir is not DirNode!")
	}

	if err := d.fs.Rename(d.id, req.OldName, newdn.id, req.NewName); err != nil {
		return err
	}

	return nil
}

func (d DirNode) Remove(ctx context.Context, req *bfuse.RemoveRequest) error {
	if err := d.fs.Remove(d.id, req.Name); err != nil {
		return err
	}

	return nil
}

func (d DirNode) Mkdir(ctx context.Context, req *bfuse.MkdirRequest) (bfs.Node, error) {
	id, err := d.fs.CreateDir(d.id, req.Name)
	if err != nil {
		return nil, err
	}

	return DirNode{fs: d.fs, id: id}, nil
}
