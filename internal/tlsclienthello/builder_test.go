package tlsclienthello

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildClientHelloBasic(t *testing.T) {
	hello := BuildClientHello("example.com")
	if hello[0] != 0x16 || hello[1] != 0x03 || hello[2] != 0x01 {
		t.Fatalf("bad tls header")
	}
	if got := int(binary.BigEndian.Uint16(hello[3:5])); got != len(hello)-5 {
		t.Fatalf("record length mismatch: got=%d want=%d", got, len(hello)-5)
	}
	if hello[5] != 0x01 {
		t.Fatalf("expected client hello")
	}
}

func TestBuildClientHelloSize(t *testing.T) {
	hello := BuildClientHello("mci.ir")
	if len(hello) != 517 {
		t.Fatalf("size mismatch: got=%d want=517", len(hello))
	}
}

func TestContainsSNI(t *testing.T) {
	sni := "auth.vercel.com"
	hello := BuildClientHello(sni)
	if !bytes.Contains(hello, []byte(sni)) {
		t.Fatalf("sni not found")
	}
}

func TestParseRoundtrip(t *testing.T) {
	sni := "auth.vercel.com"
	hello := BuildClientHello(sni)
	parsed := ParseClientHello(hello)
	if parsed["handshake_type"] != "ClientHello" {
		t.Fatalf("wrong handshake type")
	}
	if parsed["sni"] != sni {
		t.Fatalf("wrong sni")
	}
}

func TestFragmentStrategies(t *testing.T) {
	hello := BuildClientHello("test.example.com")

	sniSplit := FragmentClientHello(hello, "sni_split")
	if len(sniSplit) != 2 || !bytes.Equal(append(sniSplit[0], sniSplit[1]...), hello) {
		t.Fatalf("sni_split failed")
	}

	half := FragmentClientHello(hello, "half")
	if len(half) != 2 || !bytes.Equal(append(half[0], half[1]...), hello) {
		t.Fatalf("half failed")
	}

	multi := FragmentClientHello(hello, "multi")
	joined := make([]byte, 0, len(hello))
	for _, f := range multi {
		joined = append(joined, f...)
	}
	if len(multi) <= 2 || !bytes.Equal(joined, hello) {
		t.Fatalf("multi failed")
	}

	rec := FragmentClientHello(hello, "tls_record_frag")
	if len(rec) != 2 || rec[0][0] != 0x16 || rec[1][0] != 0x16 {
		t.Fatalf("tls_record_frag failed")
	}
}
