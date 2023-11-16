package wire

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"sync"

	"github.com/zach-klippenstein/goadb/errors"
)

// TODO(zach): All EOF errors returned from networoking calls should use ConnectionResetError.

// StatusCodes are returned by the server. If the code indicates failure, the
// next message will be the error.
const (
	StatusSuccess  string = "OKAY"
	StatusFailure  string = "FAIL"
	StatusSyncData string = "DATA"
	StatusSyncDone string = "DONE"
	StatusNone     string = ""
)

func isFailureStatus(status string) bool {
	return status == StatusFailure
}

type StatusReader interface {
	// Reads a 4-byte status string and returns it.
	// If the status string is StatusFailure, reads the error message from the server
	// and returns it as an AdbError.
	ReadStatus(req string) (string, error)
	ReadStatusWithTimeout(ctx context.Context, req string) (string, error)
}

/*
Scanner reads tokens from a server.
See Conn for more details.
*/
type Scanner interface {
	io.Closer
	StatusReader
	ReadMessage() ([]byte, error)
	ReadUntilEof() ([]byte, error)
	ReadUntilEofWithTimeout(ctx context.Context) ([]byte, error)

	NewSyncScanner() SyncScanner
}

type realScanner struct {
	reader io.ReadCloser
}

func NewScanner(r io.ReadCloser) Scanner {
	return &realScanner{r}
}

func ReadMessageString(s Scanner) (string, error) {
	msg, err := s.ReadMessage()
	if err != nil {
		return string(msg), err
	}
	return string(msg), nil
}

func (s *realScanner) ReadStatus(req string) (string, error) {
	return readStatusFailureAsError(s.reader, req, readHexLength)
}

func (s *realScanner) ReadStatusWithTimeout(ctx context.Context, req string) (string, error) {
	return readStatusFailureAsErrorWithTimeout(ctx, s.reader, req, readHexLengthWithTimeout)
}

func (s *realScanner) ReadMessage() ([]byte, error) {
	return readMessage(s.reader, readHexLength)
}

func (s *realScanner) ReadUntilEof() ([]byte, error) {
	data, err := ioutil.ReadAll(s.reader)
	if err != nil {
		return nil, errors.WrapErrorf(err, errors.NetworkError, "error reading until EOF")
	}
	return data, nil
}

func (s *realScanner) ReadUntilEofWithTimeout(ctx context.Context) ([]byte, error) {
	dataChan := make(chan []byte)
	errChan := make(chan error)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		var data []byte
		buf := make([]byte, 4096)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := s.reader.Read(buf)
				if err != nil {
					if err == io.EOF {
						dataChan <- data
						return
					} else {
						errChan <- errors.WrapErrorf(err, errors.NetworkError, "error reading until EOF")
						return
					}
				}
				data = append(data, buf[:n]...)
			}
		}
	}()

	go func() {
		wg.Wait()
		fmt.Println("ReadUntilEofWithTimeout, quiting...")
		close(dataChan)
		close(errChan)
	}()

	select {
	case data := <-dataChan:
		return data, nil
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, errors.WrapErrorf(ctx.Err(), errors.Timeout, "timeout while reading until EOF")
	}
}

func (s *realScanner) NewSyncScanner() SyncScanner {
	return NewSyncScanner(s.reader)
}

func (s *realScanner) Close() error {
	return errors.WrapErrorf(s.reader.Close(), errors.NetworkError, "error closing scanner")
}

var _ Scanner = &realScanner{}

// lengthReader is a func that readMessage uses to read message length.
// See readHexLength and readInt32.
type lengthReader func(io.Reader) (int, error)
type lengthReaderWithTimeout func(context.Context, io.Reader) (int, error)

// Reads the status, and if failure, reads the message and returns it as an error.
// If the status is success, doesn't read the message.
// req is just used to populate the AdbError, and can be nil.
// messageLengthReader is the function passed to readMessage if the status is failure.
func readStatusFailureAsError(r io.Reader, req string, messageLengthReader lengthReader) (string, error) {
	status, err := readOctetString(req, r)
	if err != nil {
		return "", errors.WrapErrorf(err, errors.NetworkError, "error reading status for %s", req)
	}

	if isFailureStatus(status) {
		msg, err := readMessage(r, messageLengthReader)
		if err != nil {
			return "", errors.WrapErrorf(err, errors.NetworkError,
				"server returned error for %s, but couldn't read the error message", req)
		}

		return "", adbServerError(req, string(msg))
	}

	return status, nil
}

func readStatusFailureAsErrorWithTimeout(ctx context.Context, r io.Reader, req string, messageLengthReader lengthReaderWithTimeout) (string, error) {
	status, err := readOctetStringWithTimeout(ctx, req, r)
	if err != nil {
		return "", errors.WrapErrorf(err, errors.NetworkError, "error reading status for %s", req)
	}

	if isFailureStatus(status) {
		msg, err := readMessageWithTimeout(ctx, r, messageLengthReader)
		if err != nil {
			return "", errors.WrapErrorf(err, errors.NetworkError,
				"server returned error for %s, but couldn't read the error message", req)
		}

		return "", adbServerError(req, string(msg))
	}

	return status, nil
}

func readOctetString(description string, r io.Reader) (string, error) {
	octet := make([]byte, 4)
	fmt.Println("readOctetString, start ReadFull, description:", description)
	n, err := io.ReadFull(r, octet)
	fmt.Println("readOctetString, ReadFull done, n:", n)

	if err == io.ErrUnexpectedEOF {
		return "", errIncompleteMessage(description, n, 4)
	} else if err != nil {
		return "", errors.WrapErrorf(err, errors.NetworkError, "error reading "+description)
	}

	return string(octet), nil
}

func readOctetStringWithTimeout(ctx context.Context, description string, r io.Reader) (string, error) {
	octet := make([]byte, 4)

	nChan := make(chan int)
	errChan := make(chan error)

	go func() {
		defer close(nChan)
		defer close(errChan)

		n, err := io.ReadFull(r, octet)
		if err == io.ErrUnexpectedEOF {
			errChan <- errIncompleteMessage(description, n, 4)
			return
		} else if err != nil {
			errChan <- errors.WrapErrorf(err, errors.NetworkError, "error reading "+description)
			return
		}

		nChan <- n
	}()

	select {
	case n := <-nChan:
		fmt.Println("readOctetStringWithTimeout, ReadFull done, n:", n)
		return string(octet), nil
	case err := <-errChan:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// func readOctetStringWithTimeout(ctx context.Context, description string, r io.Reader) (string, error) {
// 	octet := make([]byte, 4)
// 	fmt.Println("readOctetString, start Read, description:", description)

// 	buf := make([]byte, 1)

// 	for i := 0; i < 4; i++ {
// 		select {
// 		case <-ctx.Done():
// 			return "", ctx.Err()
// 		default:
// 			n, err := r.Read(buf)
// 			if err == io.ErrUnexpectedEOF {
// 				return "", errIncompleteMessage(description, n, 4)
// 			} else if err != nil {
// 				return "", errors.WrapErrorf(err, errors.NetworkError, "error reading "+description)
// 			}
// 			octet = append(octet, buf[:n]...)
// 		}
// 	}

// 	fmt.Println("readOctetString, Read done, octet:", octet)
// 	return string(octet), nil
// }

// readMessage reads a length from r, then reads length bytes and returns them.
// lengthReader is the function used to read the length. Most operations encode
// length as a hex string (readHexLength), but sync operations use little-endian
// binary encoding (readInt32).
func readMessage(r io.Reader, lengthReader lengthReader) ([]byte, error) {
	var err error

	length, err := lengthReader(r)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("readMessage, length: %d\n", length)

	data := make([]byte, length)
	n, err := io.ReadFull(r, data)

	if err != nil && err != io.ErrUnexpectedEOF {
		return data, errors.WrapErrorf(err, errors.NetworkError, "error reading message data")
	} else if err == io.ErrUnexpectedEOF {
		return data, errIncompleteMessage("message data", n, length)
	}
	return data, nil
}

func readMessageWithTimeout(ctx context.Context, r io.Reader, lengthReader lengthReaderWithTimeout) ([]byte, error) {
	var err error

	length, err := lengthReader(ctx, r)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("readMessage, length: %d\n", length)

	data := make([]byte, length)
	n, err := io.ReadFull(r, data)

	if err != nil && err != io.ErrUnexpectedEOF {
		return data, errors.WrapErrorf(err, errors.NetworkError, "error reading message data")
	} else if err == io.ErrUnexpectedEOF {
		return data, errIncompleteMessage("message data", n, length)
	}
	return data, nil
}

// readHexLength reads the next 4 bytes from r as an ASCII hex-encoded length and parses them into an int.
func readHexLength(r io.Reader) (int, error) {
	lengthHex := make([]byte, 4)
	n, err := io.ReadFull(r, lengthHex)
	if err != nil {
		return 0, errIncompleteMessage("length", n, 4)
	}

	length, err := strconv.ParseInt(string(lengthHex), 16, 64)
	if err != nil {
		return 0, errors.WrapErrorf(err, errors.NetworkError, "could not parse hex length %v", lengthHex)
	}

	// Clip the length to 255, as per the Google implementation.
	//fmt.Printf("readHexLength, length: %d\n", length)
	// if length > MaxMessageLength {
	// 	length = MaxMessageLength
	// }

	return int(length), nil
}

func readHexLengthWithTimeout(ctx context.Context, r io.Reader) (int, error) {
	lengthHex := make([]byte, 4)

	buf := make([]byte, 1)
	n := 0

	for i := 0; i < 4; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			nn, err := r.Read(buf)
			if err != nil {
				return 0, errIncompleteMessage("length", n, 4)
			}
			n += nn
			lengthHex = append(lengthHex, buf[:n]...)
		}
	}

	length, err := strconv.ParseInt(string(lengthHex), 16, 64)
	if err != nil {
		return 0, errors.WrapErrorf(err, errors.NetworkError, "could not parse hex length %v", lengthHex)
	}

	// Clip the length to 255, as per the Google implementation.
	//fmt.Printf("readHexLength, length: %d\n", length)
	// if length > MaxMessageLength {
	// 	length = MaxMessageLength
	// }

	return int(length), nil
}

// readInt32 reads the next 4 bytes from r as a little-endian integer.
// Returns an int instead of an int32 to match the lengthReader type.
func readInt32(r io.Reader) (int, error) {
	var value int32
	err := binary.Read(r, binary.LittleEndian, &value)
	return int(value), err
}

func readInt32WithTimeout(ctx context.Context, r io.Reader) (int, error) {
	var value int32

	errChan := make(chan error)
	resultChan := make(chan int)

	go func() {
		defer close(errChan)
		defer close(resultChan)

		err := binary.Read(r, binary.LittleEndian, &value)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- int(value)
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return 0, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
