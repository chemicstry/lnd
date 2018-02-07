package channeldb

import (
	"bytes"
	"io"
	"net"
	"time"
	"fmt"

	"github.com/boltdb/bolt"
	"github.com/roasbeef/btcd/btcec"
	"github.com/roasbeef/btcd/wire"
)

func serializeLinkNodeV1(w io.Writer, l *LinkNode) error {
	var buf [8]byte

	byteOrder.PutUint32(buf[:4], uint32(l.Network))
	if _, err := w.Write(buf[:4]); err != nil {
		return err
	}

	serializedID := l.IdentityPub.SerializeCompressed()
	if _, err := w.Write(serializedID); err != nil {
		return err
	}

	seenUnix := uint64(l.LastSeen.Unix())
	byteOrder.PutUint64(buf[:], seenUnix)
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}

	numAddrs := uint32(len(l.Addresses))
	byteOrder.PutUint32(buf[:4], numAddrs)
	if _, err := w.Write(buf[:4]); err != nil {
		return err
	}

	for _, addr := range l.Addresses {
		if err := serializeAddr(w, addr); err != nil {
			return err
		}
	}

	return nil
}

func deserializeLinkNodeV0(r io.Reader) (*LinkNode, error) {
	var (
		err error
		buf [8]byte
	)

	node := &LinkNode{}

	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		return nil, err
	}
	node.Network = wire.BitcoinNet(byteOrder.Uint32(buf[:4]))

	var pub [33]byte
	if _, err := io.ReadFull(r, pub[:]); err != nil {
		return nil, err
	}
	node.IdentityPub, err = btcec.ParsePubKey(pub[:], btcec.S256())
	if err != nil {
		return nil, err
	}

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}
	node.LastSeen = time.Unix(int64(byteOrder.Uint64(buf[:])), 0)

	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		return nil, err
	}
	numAddrs := byteOrder.Uint32(buf[:4])

	node.Addresses = make([]net.Addr, numAddrs)
	for i := uint32(0); i < numAddrs; i++ {
		addrString, err := wire.ReadVarString(r, 0)
		if err != nil {
			return nil, err
		}

		// We use the general resolveTCP function in case a separate
		// resolver was specified in the SetResolver function. By
		// default resolveTCP = net.ResolveTCPAddr.
		addr, err := net.ResolveTCPAddr("tcp", addrString)
		if err != nil {
			return nil, err
		}
		node.Addresses[i] = addr
	}

	return node, nil
}

func AddressMigration(tx *bolt.Tx) error {
	nodeMetaBucket := tx.Bucket(nodeInfoBucket)
	if nodeMetaBucket == nil {
		fmt.Errorf("Nodes bucket not found")
	}

	err := nodeMetaBucket.ForEach(func(k, v []byte) error {
		if v == nil {
			return nil
		}

		nodeReader := bytes.NewReader(v)
		linkNode, err := deserializeLinkNodeV0(nodeReader)
		if err != nil {
			return err
		}

		var b bytes.Buffer
		if err = serializeLinkNode(&b, linkNode); err != nil {
			return err
		}

		return nodeMetaBucket.Put(k, b.Bytes())
	})

	return err
}
