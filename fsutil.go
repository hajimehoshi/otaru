package otaru

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nyaxt/otaru/inodedb"
)

func (fs *FileSystem) FindDirFullPath(fullpath string) (inodedb.ID, error) {
	if len(fullpath) < 1 || fullpath[0] != '/' {
		return 0, fmt.Errorf("Path must start with /, but given: %v", fullpath)
	}

	if fullpath != "/" {
		panic("FIXME: implement me!!!!")
	}

	return inodedb.ID(1), nil
}

func (fs *FileSystem) OpenFileFullPath(fullpath string, flags int, perm os.FileMode) (*FileHandle, error) {
	perm &= os.ModePerm

	if len(fullpath) < 1 || fullpath[0] != '/' {
		return nil, fmt.Errorf("Path must start with /, but given: %v", fullpath)
	}

	dirname := filepath.Dir(fullpath)
	basename := filepath.Base(fullpath)

	dirID, err := fs.FindDirFullPath(dirname)
	if err != nil {
		return nil, err
	}

	entries, err := fs.DirEntries(dirID)
	if err != nil {
		return nil, err
	}

	var id inodedb.ID
	id, ok := entries[basename]
	if !ok {
		if flags|os.O_CREATE != 0 {
			// FIXME: apply perm

			id, err = fs.CreateFile(dirID, basename)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, ENOENT
		}
	}

	if id == 0 {
		panic("inode id must != 0 here!")
	}

	// FIXME: handle flag
	fh, err := fs.OpenFile(id, flags)
	if err != nil {
		return nil, err
	}

	return fh, nil
}
