/*
Copyright 2011 The Perkeep Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package localdisk

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"perkeep.org/pkg/blob"
)

func (ds *DiskStorage) startGate() {
	if ds.tmpFileGate == nil {
		return
	}
	ds.tmpFileGate.Start()
}

func (ds *DiskStorage) doneGate() {
	if ds.tmpFileGate == nil {
		return
	}
	ds.tmpFileGate.Done()
}

func (ds *DiskStorage) ReceiveBlob(blobRef blob.Ref, source io.Reader) (ref blob.SizedRef, err error) {
	ds.dirLockMu.RLock()
	defer ds.dirLockMu.RUnlock()

	hashedDirectory := ds.blobDirectory(blobRef)
	err = os.MkdirAll(hashedDirectory, 0700)
	if err != nil {
		return
	}

	// TODO(mpl): warn when we hit the gate, and at a limited rate, like maximum once a minute.
	// Deferring to another CL, since it requires modifications to syncutil.Gate first.
	ds.startGate()
	tempFile, err := ioutil.TempFile(hashedDirectory, blobFileBaseName(blobRef)+".tmp")
	if err != nil {
		ds.doneGate()
		return
	}

	success := false // set true later
	defer func() {
		if !success {
			log.Println("Removing temp file: ", tempFile.Name())
			os.Remove(tempFile.Name())
		}
		ds.doneGate()
	}()

	written, err := io.Copy(tempFile, source)
	if err != nil {
		return
	}
	if err = tempFile.Sync(); err != nil {
		return
	}
	if err = tempFile.Close(); err != nil {
		return
	}
	stat, err := os.Lstat(tempFile.Name())
	if err != nil {
		return
	}
	if stat.Size() != written {
		err = fmt.Errorf("temp file %q size %d didn't match written size %d", tempFile.Name(), stat.Size(), written)
		return
	}

	fileName := ds.blobPath(blobRef)
	if err = os.Rename(tempFile.Name(), fileName); err != nil {
		if err = mapRenameError(err, tempFile.Name(), fileName); err != nil {
			return
		}
	}

	stat, err = os.Lstat(fileName)
	if err != nil {
		return
	}
	if stat.Size() != written {
		err = errors.New("written size didn't match")
		return
	}

	success = true // used in defer above
	return blob.SizedRef{Ref: blobRef, Size: uint32(stat.Size())}, nil
}
