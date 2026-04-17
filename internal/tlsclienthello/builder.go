package tlsclienthello

import (
	"crypto/rand"
	"encoding/binary"
)

var cipherSuites = mustHex(
	"0024" +
		"130213031301c02cc030c02bc02fcca9cca8c024c028c023c027009f009e006b006700ff",
)

var supportedGroups = mustHex("000a00160014001d0017001e0019001801000101010201030104")
var signatureAlgorithms = mustHex("000d002a0028040305030603080708080809080a080b080408050806040105010601030303010302040205020602")
var ecPointFormats = mustHex("000b000403000102")
var sessionTicket = mustHex("00230000")
var alpn = mustHex("0010000e000c02683208687474702f312e31")
var encryptThenMAC = mustHex("00160000")
var extendedMasterSecret = mustHex("00170000")
var supportedVersions = mustHex("002b00050403040303")
var pskKeyExchange = mustHex("002d00020101")

func BuildSNIExtension(sni string) []byte {
	name := []byte(sni)
	entry := make([]byte, 1+2+len(name))
	entry[0] = 0
	binary.BigEndian.PutUint16(entry[1:3], uint16(len(name)))
	copy(entry[3:], name)

	list := make([]byte, 2+len(entry))
	binary.BigEndian.PutUint16(list[0:2], uint16(len(entry)))
	copy(list[2:], entry)

	out := make([]byte, 4+len(list))
	binary.BigEndian.PutUint16(out[0:2], 0x0000)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(list)))
	copy(out[4:], list)
	return out
}

func BuildKeyShareExtension(publicKey []byte) []byte {
	if len(publicKey) == 0 {
		publicKey = make([]byte, 32)
		_, _ = rand.Read(publicKey)
	}
	entry := make([]byte, 2+2+len(publicKey))
	binary.BigEndian.PutUint16(entry[0:2], 0x001D)
	binary.BigEndian.PutUint16(entry[2:4], uint16(len(publicKey)))
	copy(entry[4:], publicKey)

	data := make([]byte, 2+len(entry))
	binary.BigEndian.PutUint16(data[0:2], uint16(len(entry)))
	copy(data[2:], entry)

	out := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(out[0:2], 0x0033)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(data)))
	copy(out[4:], data)
	return out
}

func buildPaddingExtension(targetLength, currentLength int) []byte {
	pad := targetLength - currentLength - 4
	if pad < 0 {
		return nil
	}
	out := make([]byte, 4+pad)
	binary.BigEndian.PutUint16(out[0:2], 0x0015)
	binary.BigEndian.PutUint16(out[2:4], uint16(pad))
	return out
}

func BuildClientHello(sni string) []byte {
	return BuildClientHelloFull(sni, nil, nil, nil, 517)
}

func BuildClientHelloFull(sni string, sessionID, randomBytes, keyShare []byte, targetSize int) []byte {
	if len(sessionID) == 0 {
		sessionID = make([]byte, 32)
		_, _ = rand.Read(sessionID)
	}
	if len(randomBytes) == 0 {
		randomBytes = make([]byte, 32)
		_, _ = rand.Read(randomBytes)
	}

	clientVersion := []byte{0x03, 0x03}
	sidField := append([]byte{byte(len(sessionID))}, sessionID...)
	compression := []byte{0x01, 0x00}

	exts := make([]byte, 0, 512)
	exts = append(exts, BuildSNIExtension(sni)...)
	exts = append(exts, ecPointFormats...)
	exts = append(exts, supportedGroups...)
	exts = append(exts, sessionTicket...)
	exts = append(exts, alpn...)
	exts = append(exts, encryptThenMAC...)
	exts = append(exts, extendedMasterSecret...)
	exts = append(exts, signatureAlgorithms...)
	exts = append(exts, supportedVersions...)
	exts = append(exts, pskKeyExchange...)
	exts = append(exts, BuildKeyShareExtension(keyShare)...)

	handshakeBodyNoPad := append([]byte{}, clientVersion...)
	handshakeBodyNoPad = append(handshakeBodyNoPad, randomBytes...)
	handshakeBodyNoPad = append(handshakeBodyNoPad, sidField...)
	handshakeBodyNoPad = append(handshakeBodyNoPad, cipherSuites...)
	handshakeBodyNoPad = append(handshakeBodyNoPad, compression...)

	totalSoFar := 4 + len(handshakeBodyNoPad) + 2 + len(exts)
	recordSoFar := 5 + totalSoFar
	exts = append(exts, buildPaddingExtension(targetSize, recordSoFar)...)

	extWithLen := make([]byte, 2+len(exts))
	binary.BigEndian.PutUint16(extWithLen[0:2], uint16(len(exts)))
	copy(extWithLen[2:], exts)

	handshakeBody := append(handshakeBodyNoPad, extWithLen...)
	hsLen := len(handshakeBody)
	handshake := []byte{0x01, byte(hsLen >> 16), byte(hsLen >> 8), byte(hsLen)}
	handshake = append(handshake, handshakeBody...)

	out := []byte{0x16, 0x03, 0x01, 0x00, 0x00}
	binary.BigEndian.PutUint16(out[3:5], uint16(len(handshake)))
	out = append(out, handshake...)
	return out
}

func BuildClientResponse(randomBytes []byte) []byte {
	if len(randomBytes) == 0 {
		randomBytes = make([]byte, 32)
		_, _ = rand.Read(randomBytes)
	}
	ccs := []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01}
	app := []byte{0x17, 0x03, 0x03, 0x00, 0x00}
	binary.BigEndian.PutUint16(app[3:5], uint16(len(randomBytes)))
	app = append(app, randomBytes...)
	return append(ccs, app...)
}

func ParseClientHello(data []byte) map[string]any {
	out := map[string]any{}
	if len(data) < 5 {
		return out
	}
	contentType := data[0]
	out["content_type"] = int(contentType)
	out["tls_version"] = binary.BigEndian.Uint16(data[1:3])
	if contentType != 0x16 || len(data) < 9 {
		return out
	}
	pos := 5
	if data[pos] != 0x01 || pos+4 > len(data) {
		return out
	}
	out["handshake_type"] = "ClientHello"
	pos += 4
	if pos+2+32 > len(data) {
		return out
	}
	out["client_version"] = binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2 + 32
	if pos >= len(data) {
		return out
	}
	sidLen := int(data[pos])
	pos++
	if pos+sidLen+2 > len(data) {
		return out
	}
	pos += sidLen
	csLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2 + csLen
	if pos >= len(data) {
		return out
	}
	compLen := int(data[pos])
	pos += 1 + compLen
	if pos+2 > len(data) {
		return out
	}
	extLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2
	extEnd := pos + extLen
	if extEnd > len(data) {
		extEnd = len(data)
	}
	for pos+4 <= extEnd {
		extType := binary.BigEndian.Uint16(data[pos : pos+2])
		l := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4
		if pos+l > extEnd {
			break
		}
		extData := data[pos : pos+l]
		pos += l
		if extType == 0 && len(extData) >= 5 {
			nameLen := int(binary.BigEndian.Uint16(extData[3:5]))
			if 5+nameLen <= len(extData) {
				out["sni"] = string(extData[5 : 5+nameLen])
			}
		}
	}
	return out
}

func mustHex(s string) []byte {
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		out[i] = fromHex(s[2*i])<<4 | fromHex(s[2*i+1])
	}
	return out
}

func fromHex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
