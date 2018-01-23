package channeldb

import (
	"fmt"
	"io"
	"bytes"
	"encoding/json"
	"net/http"
	"io/ioutil"
	"time"

	"github.com/boltdb/bolt"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/roasbeef/btcd/wire"
)

type SmartBitApiResponse struct {
	Success	bool `json:"success"`
	Tx		struct {
		TxID		string `json:"txid"`
		Block		uint32 `json:"block"`
		TxIndex	uint32 `json:"tx_index"`
		BlockIndex uint32 `json:"block_index"`
	} `json:"transaction"`
}

func getShortChanID(outPoint wire.OutPoint) (*lnwire.ShortChannelID, error) {
	shortChanID := &lnwire.ShortChannelID{}

	url := "https://api.smartbit.com.au/v1/blockchain/tx/" + outPoint.Hash.String()

    spaceClient := http.Client{
        Timeout: time.Second * 10,
    }

    req, err := http.NewRequest(http.MethodGet, url, nil)
    if err != nil {
        return nil, fmt.Errorf("http request failed")
    }

    req.Header.Set("User-Agent", "Reckless-LN-Migrations")

    res, getErr := spaceClient.Do(req)
    if getErr != nil {
        return nil, fmt.Errorf("http request failed")
    }

    body, readErr := ioutil.ReadAll(res.Body)
    if readErr != nil {
        return nil, fmt.Errorf("request read failed")
	}

    jsonapi := SmartBitApiResponse{}
    jsonErr := json.Unmarshal(body, &jsonapi)
    if jsonErr != nil {
        return nil, fmt.Errorf("json decode failed")
    }

	fmt.Printf("%+v\n", jsonapi)

	shortChanID.BlockHeight = jsonapi.Tx.Block
	shortChanID.TxIndex = jsonapi.Tx.BlockIndex
	shortChanID.TxPosition = uint16(outPoint.Index)
	
	return shortChanID, nil
}

func deserializeCloseChannelSummaryV0(r io.Reader) (*ChannelCloseSummary, error) {
	c := &ChannelCloseSummary{}

	err := readElements(r,
		&c.ChanPoint, &c.ChainHash, &c.ClosingTXID, &c.CloseHeight,
		&c.RemotePub, &c.Capacity, &c.SettledBalance,
		&c.TimeLockedBalance, &c.CloseType, &c.IsPending,
	)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func serializeChannelCloseSummaryV1(w io.Writer, cs *ChannelCloseSummary) error {
	return writeElements(w,
		cs.ChanPoint, cs.ShortChanID, cs.ChainHash, cs.ClosingTXID,
		cs.CloseHeight, cs.RemotePub, cs.Capacity, cs.SettledBalance,
		cs.TimeLockedBalance, cs.CloseType, cs.IsPending,
	)
}

func ShortChanIDMigration(tx *bolt.Tx) error {

	closeBucket := tx.Bucket(closedChannelBucket)
	if closeBucket == nil {
		return ErrNoClosedChannels
	}

	err := closeBucket.ForEach(func(chanID []byte, summaryBytes []byte) error {
		summaryReader := bytes.NewReader(summaryBytes)
		chanSummary, err := deserializeCloseChannelSummaryV0(summaryReader)
		if err != nil {
			return err
		}

		fmt.Printf("Hash: %s, Index: %d\n", chanSummary.ChanPoint.Hash, chanSummary.ChanPoint.Index)

		shortChanID, err := getShortChanID(chanSummary.ChanPoint)

		if err != nil {
			return err
		}

		chanSummary.ShortChanID = *shortChanID

		fmt.Printf("%+v\n", chanSummary)

		var newSummary bytes.Buffer
		err = serializeChannelCloseSummaryV1(&newSummary, chanSummary)

		if err != nil {
			return err
		}

		err = closeBucket.Put(chanID, newSummary.Bytes())
		
		if err != nil {
			return err
		}
		
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}
