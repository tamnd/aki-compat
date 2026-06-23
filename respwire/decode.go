package respwire

import (
	"bufio"
	"fmt"
	"strconv"
)

// decode reads one complete RESP value from r. It blocks until a full value is
// available or the read fails. It handles every RESP2 and RESP3 type the harness
// can see in a reply.
func decode(r *bufio.Reader) (Value, error) {
	typeByte, err := r.ReadByte()
	if err != nil {
		return Value{}, err
	}
	switch Kind(typeByte) {
	case KindSimpleString:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindSimpleString, Str: line}, nil

	case KindError:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindError, Str: line}, nil

	case KindInteger:
		n, err := readInt(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindInteger, Int: n}, nil

	case KindBulkString:
		return decodeBulk(r, KindBulkString)

	case KindBulkError:
		v, err := decodeBulk(r, KindBulkError)
		if err != nil {
			return Value{}, err
		}
		// A bulk error carries its text in Str; mirror it for IsError callers.
		return v, nil

	case KindVerbatim:
		v, err := decodeBulk(r, KindVerbatim)
		if err != nil {
			return Value{}, err
		}
		if len(v.Str) < 4 || v.Str[3] != ':' {
			return Value{}, fmt.Errorf("respwire: malformed verbatim string")
		}
		v.VerbEnc = v.Str[:3]
		v.Str = v.Str[4:]
		return v, nil

	case KindArray:
		return decodeAggregate(r, KindArray)

	case KindSet:
		return decodeAggregate(r, KindSet)

	case KindPush:
		return decodeAggregate(r, KindPush)

	case KindMap:
		return decodeMap(r)

	case KindNull:
		if _, err := readLine(r); err != nil {
			return Value{}, err
		}
		return Value{Kind: KindNull, NullFrom: '_'}, nil

	case KindBool:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		if line != "t" && line != "f" {
			return Value{}, fmt.Errorf("respwire: malformed boolean %q", line)
		}
		return Value{Kind: KindBool, Bool: line == "t"}, nil

	case KindDouble:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindDouble, Double: line}, nil

	case KindBigNumber:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindBigNumber, Big: line}, nil

	default:
		return Value{}, fmt.Errorf("respwire: unexpected type byte %q", typeByte)
	}
}

func decodeBulk(r *bufio.Reader, k Kind) (Value, error) {
	n, err := readInt(r)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		// $-1 is the RESP2 null bulk string.
		return Value{Kind: KindNull, NullFrom: '$'}, nil
	}
	buf := make([]byte, n+2)
	if _, err := readFull(r, buf); err != nil {
		return Value{}, err
	}
	if buf[n] != '\r' || buf[n+1] != '\n' {
		return Value{}, fmt.Errorf("respwire: missing CRLF after bulk")
	}
	v := Value{Kind: k, Str: string(buf[:n])}
	return v, nil
}

func decodeAggregate(r *bufio.Reader, k Kind) (Value, error) {
	n, err := readInt(r)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		// *-1 is the RESP2 null array.
		return Value{Kind: KindNull, NullFrom: '*'}, nil
	}
	elems := make([]Value, 0, n)
	for range n {
		e, err := decode(r)
		if err != nil {
			return Value{}, err
		}
		elems = append(elems, e)
	}
	return Value{Kind: k, Elems: elems}, nil
}

func decodeMap(r *bufio.Reader) (Value, error) {
	n, err := readInt(r)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{Kind: KindNull, NullFrom: '%'}, nil
	}
	pairs := make([][2]Value, 0, n)
	for range n {
		key, err := decode(r)
		if err != nil {
			return Value{}, err
		}
		val, err := decode(r)
		if err != nil {
			return Value{}, err
		}
		pairs = append(pairs, [2]Value{key, val})
	}
	return Value{Kind: KindMap, Map: pairs}, nil
}

// readLine returns the text up to the next CRLF, without the CRLF.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	n := len(line)
	if n < 2 || line[n-2] != '\r' {
		return "", fmt.Errorf("respwire: line not CRLF terminated")
	}
	return line[:n-2], nil
}

func readInt(r *bufio.Reader) (int64, error) {
	line, err := readLine(r)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("respwire: bad integer %q", line)
	}
	return n, nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := r.Read(buf[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}
