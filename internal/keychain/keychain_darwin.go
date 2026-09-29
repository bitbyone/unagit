//go:build darwin

package keychain

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <stdlib.h>
#include <Security/Security.h>

// The legacy keychain calls are the ones that give an item an access list
// trusting only the application that created it; the newer SecItem API
// with the data protection keychain needs a signed application with an
// entitlement.

static OSStatus kc_find(const char *service, UInt32 serviceLen, const char *account, UInt32 accountLen,
                        void **data, UInt32 *dataLen, SecKeychainItemRef *item) {
	return SecKeychainFindGenericPassword(NULL, serviceLen, service, accountLen, account, dataLen, data, item);
}

static OSStatus kc_add(const char *service, UInt32 serviceLen, const char *account, UInt32 accountLen,
                       const void *data, UInt32 dataLen) {
	return SecKeychainAddGenericPassword(NULL, serviceLen, service, accountLen, account, dataLen, data, NULL);
}

static OSStatus kc_update(SecKeychainItemRef item, const void *data, UInt32 dataLen) {
	return SecKeychainItemModifyAttributesAndData(item, NULL, dataLen, data);
}

static OSStatus kc_delete(SecKeychainItemRef item) {
	return SecKeychainItemDelete(item);
}

static void kc_free(void *data) {
	SecKeychainItemFreeContent(NULL, data);
}

static void kc_release(SecKeychainItemRef item) {
	if (item) CFRelease(item);
}

static char *kc_message(OSStatus status) {
	CFStringRef text = SecCopyErrorMessageString(status, NULL);
	if (!text) return NULL;
	CFIndex size = CFStringGetMaximumSizeForEncoding(CFStringGetLength(text), kCFStringEncodingUTF8) + 1;
	char *out = malloc(size);
	if (!CFStringGetCString(text, out, size, kCFStringEncodingUTF8)) { free(out); out = NULL; }
	CFRelease(text);
	return out;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const errItemNotFound = -25300 // errSecItemNotFound

// Available reports whether secrets can be kept in a keychain here.
func Available() bool { return true }

func failure(what string, status C.OSStatus) error {
	if status == errItemNotFound {
		return ErrNotFound
	}
	msg := fmt.Sprintf("status %d", int(status))
	if text := C.kc_message(status); text != nil {
		msg = C.GoString(text)
		C.free(unsafe.Pointer(text))
	}
	return fmt.Errorf("keychain %s: %s", what, msg)
}

type name struct {
	service, account *C.char
	sLen, aLen       C.UInt32
}

func newName(service, account string) name {
	return name{C.CString(service), C.CString(account), C.UInt32(len(service)), C.UInt32(len(account))}
}

func (n name) free() {
	C.free(unsafe.Pointer(n.service))
	C.free(unsafe.Pointer(n.account))
}

// Get returns the secret stored under service and account. The caller should
// wipe it when done.
func Get(service, account string) ([]byte, error) {
	n := newName(service, account)
	defer n.free()
	var data unsafe.Pointer
	var length C.UInt32
	status := C.kc_find(n.service, n.sLen, n.account, n.aLen, &data, &length, nil)
	if status != 0 {
		return nil, failure("read", status)
	}
	defer C.kc_free(data)
	return C.GoBytes(data, C.int(length)), nil
}

// Set stores the secret under service and account, replacing what was there.
func Set(service, account string, secret []byte) error {
	n := newName(service, account)
	defer n.free()
	data := C.CBytes(secret)
	defer func() {
		// The copy handed to C holds the secret too.
		b := unsafe.Slice((*byte)(data), len(secret))
		for i := range b {
			b[i] = 0
		}
		C.free(data)
	}()
	var item C.SecKeychainItemRef
	status := C.kc_find(n.service, n.sLen, n.account, n.aLen, nil, nil, &item)
	switch {
	case status == 0:
		defer C.kc_release(item)
		if status := C.kc_update(item, data, C.UInt32(len(secret))); status != 0 {
			return failure("update", status)
		}
		return nil
	case status == errItemNotFound:
		if status := C.kc_add(n.service, n.sLen, n.account, n.aLen, data, C.UInt32(len(secret))); status != 0 {
			return failure("store", status)
		}
		return nil
	default:
		return failure("look up", status)
	}
}

// Delete removes what is stored under service and account. Nothing there is
// not an error.
func Delete(service, account string) error {
	n := newName(service, account)
	defer n.free()
	var item C.SecKeychainItemRef
	status := C.kc_find(n.service, n.sLen, n.account, n.aLen, nil, nil, &item)
	if status == errItemNotFound {
		return nil
	}
	if status != 0 {
		return failure("look up", status)
	}
	defer C.kc_release(item)
	if status := C.kc_delete(item); status != 0 {
		return failure("delete", status)
	}
	return nil
}
