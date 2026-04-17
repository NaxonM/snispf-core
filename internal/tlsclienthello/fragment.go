package tlsclienthello

import "encoding/binary"

func FragmentClientHello(data []byte, strategy string) [][]byte {
	if strategy == "none" || len(data) < 10 {
		return [][]byte{data}
	}
	switch strategy {
	case "sni_split":
		return fragmentAtSNI(data)
	case "half":
		mid := len(data) / 2
		return [][]byte{data[:mid], data[mid:]}
	case "multi":
		return fragmentMulti(data, 24)
	case "tls_record_frag":
		return tlsRecordFragment(data)
	default:
		return [][]byte{data}
	}
}

func FindSNIOffset(data []byte) (int, int) {
	for pos := 0; pos < len(data)-10; pos++ {
		if data[pos] != 0x00 || data[pos+1] != 0x00 {
			continue
		}
		extLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if extLen <= 4 || extLen >= 256 || pos+9 >= len(data) {
			continue
		}
		nameType := data[pos+6]
		nameLen := int(binary.BigEndian.Uint16(data[pos+7 : pos+9]))
		if nameType == 0 && nameLen > 0 && nameLen < 256 && pos+9+nameLen <= len(data) {
			return pos + 9, nameLen
		}
	}
	return -1, 0
}

func fragmentAtSNI(data []byte) [][]byte {
	off, l := FindSNIOffset(data)
	if off < 0 {
		mid := len(data) / 2
		return [][]byte{data[:mid], data[mid:]}
	}
	split := off + l/2
	return [][]byte{data[:split], data[split:]}
}

func fragmentMulti(data []byte, chunk int) [][]byte {
	out := make([][]byte, 0, len(data)/chunk+1)
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		out = append(out, data[i:end])
	}
	return out
}

func tlsRecordFragment(data []byte) [][]byte {
	if len(data) < 6 || data[0] != 0x16 {
		return [][]byte{data}
	}
	ver := data[1:3]
	hs := data[5:]
	mid := len(hs) / 2
	p1 := hs[:mid]
	p2 := hs[mid:]
	r1 := append([]byte{0x16, ver[0], ver[1], 0, 0}, p1...)
	r2 := append([]byte{0x16, ver[0], ver[1], 0, 0}, p2...)
	binary.BigEndian.PutUint16(r1[3:5], uint16(len(p1)))
	binary.BigEndian.PutUint16(r2[3:5], uint16(len(p2)))
	return [][]byte{r1, r2}
}

func FragmentData(data []byte, sizes []int) [][]byte {
	out := make([][]byte, 0, len(sizes))
	pos := 0
	for i, s := range sizes {
		if pos >= len(data) {
			break
		}
		if i == len(sizes)-1 {
			out = append(out, data[pos:])
			return out
		}
		end := pos + s
		if end > len(data) {
			end = len(data)
		}
		out = append(out, data[pos:end])
		pos = end
	}
	if len(out) == 0 {
		return [][]byte{data}
	}
	if pos < len(data) {
		out = append(out, data[pos:])
	}
	return out
}
