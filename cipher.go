package main

// Rc Bitwise rotate-left 32-bit
func Rc(t uint32, e uint) uint32 {
	return (t << e) | (t >> (32 - e))
}

// Ec Rozclub Custom Stream Cipher (Xoroshiro128** with FNV-1a Key Derivation)
type Ec struct {
	s [4]uint32
	w uint32
	n int
}

func NewEc(role byte, keyBytes17 []byte) *Ec {
	var n uint32 = 2166136261
	fnv := func(b byte) {
		n = (n ^ uint32(b)) * 16777619
	}

	key := "THc-R0z#5e2b-91d0:StreamKey!2026"
	for i := 0; i < len(key); i++ {
		fnv(key[i])
	}
	fnv(role)
	for i := 0; i < 17; i++ {
		fnv(keyBytes17[i])
	}

	c := &Ec{}
	for a := 0; a < 4; a++ {
		t := n + uint32(a+1)*2654435769
		t = (t ^ (t >> 16)) * 2246822507
		t = (t ^ (t >> 13)) * 3266489909
		t = t ^ (t >> 16)
		if t == 0 {
			t = 1831565813
		}
		c.s[a] = t
	}
	c.w = 0
	c.n = 0
	for a := 0; a < 16; a++ {
		c.next()
	}
	return c
}

func (c *Ec) next() uint32 {
	t := &c.s
	e := (Rc(t[1]*5, 7)) * 9
	n := t[1] << 9
	t[2] ^= t[0]
	t[3] ^= t[1]
	t[1] ^= t[2]
	t[0] ^= t[3]
	t[2] ^= n
	t[3] = Rc(t[3], 11)
	return e
}

func (c *Ec) Xor(buf []byte) {
	for i := 0; i < len(buf); i++ {
		if c.n == 0 {
			c.w = c.next()
			c.n = 4
		}
		buf[i] ^= byte(c.w & 0xFF)
		c.w >>= 8
		c.n--
	}
}
