//go:build windows

package adopat

import (
	"unicode/utf16"

	"github.com/danieljoos/wincred"
)

// lookupStore reads a generic credential from the Windows Credential Manager.
// The azure-devops extension writes it through Python's keyring, whose
// WinVaultKeyring backend uses the service name verbatim as the target name.
func lookupStore(service string) (string, error) {
	if service == "" {
		return "", ErrNotFound
	}
	cred, err := wincred.GetGenericCredential(service)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", ErrNotFound
	}
	return decodeCredentialBlob(cred.CredentialBlob), nil
}

// decodeCredentialBlob converts a credential blob to a string. Python's keyring
// writes UTF-16LE; other writers (and our own future writes) may use UTF-8, so
// the encoding is detected rather than assumed.
func decodeCredentialBlob(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if !looksUTF16LE(b) {
		return string(b)
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// looksUTF16LE reports whether b is UTF-16LE-encoded ASCII text, which for a
// PAT (base32/base64 characters only) means every second byte is zero.
func looksUTF16LE(b []byte) bool {
	if len(b) < 2 || len(b)%2 != 0 {
		return false
	}
	for i := 1; i < len(b); i += 2 {
		if b[i] != 0 {
			return false
		}
	}
	return true
}
