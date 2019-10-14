package sproto

func packSegment(dst []byte, src []byte, ffN int) int {
	var header uint8 = 0
	notzero := 0
	for i, v := range src {
		if v != 0 {
			notzero++
			header |= (1 << uint(i))
			dst[notzero] = v
		}
	}

	if ffN > 0 {
		if notzero >= 6 {
			return 8
		}
	}

	if notzero == 8 {
		return 10
	}

	dst[0] = header
	return notzero + 1
}

func writeFF(dst, src []byte, n int) {
	align8 := (n + 7) & (^7)
	dst[0] = 0xff
	dst[1] = uint8(align8/8 - 1)
	copy(dst[2:], src[:n])
	for i := n; i < align8; i++ {
		dst[2+i] = 0
	}
}

func Pack(src []byte) []byte {
	// the worst-case space overhead of packing is 2 bytes per 16 bytes of input
	maxsz := len(src) + ((len(src)+2047)/2048)*100 + 2
	packed := make([]byte, maxsz, maxsz)
	offset := 0

	var ffS []byte
	var ffN, ffP int
	for len(src) > 0 {
		var input []byte
		if len(src) >= 8 {
			input = src[:8]
		} else {
			input = src
		}
		used := packSegment(packed[offset:], input, ffN)
		if used == 10 {
			ffS = src
			ffP = offset
			ffN = 1
		} else if used == 8 && ffN > 0 {
			ffN += 1
			if ffN == 256 {
				writeFF(packed[ffP:], ffS, ffN*8)
				ffN = 0
			}
		} else {
			if ffN > 0 {
				writeFF(packed[ffP:], ffS, ffN*8)
				ffN = 0
			}
		}
		offset += used
		if len(src) <= 8 {
			break
		}
		src = src[8:]
	}

	if ffN > 0 {
		writeFF(packed[ffP:], ffS, len(ffS))
	}
	return packed[:offset]
}

func Unpack(src []byte) ([]byte, error) {
	unpacked := make([]byte, 0, len(src))
	buf := make([]byte, 8)
	for len(src) > 0 {
		sz := len(src)
		used := 0
		header := src[0]
		if header == 0xff {
			if sz < 2 {
				return nil, ErrUnpack
			}
			used = 2 + (int(src[1])+1)*8
			if sz < used {
				return nil, ErrUnpack
			}
			unpacked = Append(unpacked, src[2:used])
		} else {
			notzero := 0
			for i := 0; i < 8; i++ {
				if header%2 == 1 {
					notzero++
					if sz < notzero+1 {
						return nil, ErrUnpack
					}
					buf[i] = src[notzero]
				} else {
					buf[i] = 0
				}
				header = header >> 1
			}
			used = notzero + 1
			unpacked = Append(unpacked, buf)
		}
		src = src[used:]
	}
	return unpacked, nil
}
