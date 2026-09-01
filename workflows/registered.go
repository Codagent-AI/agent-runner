package builtinworkflows

import (
	"io/fs"
	"sync"
	"testing/fstest"
)

var registered struct {
	sync.RWMutex
	files map[string][]byte
}

// RegisterBuiltinAsset adds a build-specific builtin workflow definition.
// Callers use it from a build-constrained package init; ordinary builds never
// register development-only assets.
func RegisterBuiltinAsset(relPath string, data []byte) {
	registered.Lock()
	defer registered.Unlock()
	if registered.files == nil {
		registered.files = make(map[string][]byte)
	}
	registered.files[relPath] = append([]byte(nil), data...)
}

func registeredFile(relPath string) ([]byte, bool) {
	registered.RLock()
	defer registered.RUnlock()
	data, ok := registered.files[relPath]
	return append([]byte(nil), data...), ok
}

func registeredFS() fs.FS {
	registered.RLock()
	defer registered.RUnlock()
	if len(registered.files) == 0 {
		return nil
	}
	files := make(fstest.MapFS, len(registered.files))
	for name, data := range registered.files {
		files[name] = &fstest.MapFile{Data: append([]byte(nil), data...)}
	}
	return files
}

// Sources returns every builtin source visible to discovery. The base embedded
// filesystem is always first so build-specific assets retain normal builtin
// discovery semantics without changing source precedence.
func Sources() []fs.FS {
	sources := []fs.FS{FS}
	if extra := registeredFS(); extra != nil {
		sources = append(sources, extra)
	}
	return sources
}
