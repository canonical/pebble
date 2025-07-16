// Copyright (c) 2025 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

//go:build cgo && linux

package cryptutil

//#cgo pkg-config: libcrypt
//#include <crypt.h>
//#include <string.h>
//#include <malloc.h>
//struct crypt_data *alloc_cryptdata() {
//  struct crypt_data *d = malloc(sizeof(struct crypt_data));
//  if(d == 0) {
//    return 0;
//  }
//  memset(d, 0, sizeof(struct crypt_data));
//  return d;
//}
import "C"

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"
)

var (
	cryptNotCalled atomic.Bool
)

func crypt6_verify(hashedKey string, key string) error {
	cryptNotCalled.Store(false)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lastSep := strings.LastIndex(hashedKey, "$")
	if lastSep < 0 {
		return errors.New("ill-formed key hash")
	}
	salt := hashedKey[:lastSep]
	csalt := C.CString(salt)
	defer C.free(unsafe.Pointer(csalt))
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	ccrypt_data := C.alloc_cryptdata()
	if ccrypt_data == nil {
		panic("cannot alloc crypt_data")
	}
	defer C.free(unsafe.Pointer(ccrypt_data))
	chashedkey, err := C.crypt_r(ckey, csalt, ccrypt_data)
	if err != nil {
		return fmt.Errorf("unable to verify passwd: %w", err)
	}
	resHashedKey := C.GoString(chashedkey)
	if subtle.ConstantTimeCompare([]byte(resHashedKey), []byte(hashedKey)) == 1 {
		return nil
	}
	return errors.New("unable to verify passwd")
}
