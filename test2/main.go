package main

import (
	"fmt"
	"time"

	"github.com/mdigger/smpp"
)

func main() {
	// connect and bind
	trx, err := smpp.NewTransceiver(
		"67.231.4.201:2775",
		time.Second*10,
		smpp.Params{
			"system_type": "SMPP",
			"system_id":   "Zultys",
			"password":    "unmQF932",
		},
	)
	if err != nil {
		fmt.Println("Connection Err:", err)
		return
	}

	// Send SubmitSm
	seq, err := trx.SubmitSm("4086751455", "4154292837",
		fmt.Sprintf("Test message with time: %v", time.Now().Format(time.RFC822)), &smpp.Params{})
	// Pdu gen errors
	if err != nil {
		fmt.Println("SubmitSm err:", err)
	}
	// Should save this to match with message_id
	fmt.Println("seq:", seq)

	// start reading PDUs
	for {
		pdu, err := trx.Read() // This is blocking
		if err != nil {
			break
		}
		// Transceiver auto handles EnquireLinks
		switch pdu.GetHeader().Id {
		case smpp.SUBMIT_SM_RESP:
			// message_id should match this with seq message
			fmt.Println("SUBMIT_SM_RESP ID:", pdu.GetField("message_id").String())
		case smpp.DELIVER_SM:
			// received Deliver Sm
			fmt.Println("DELIVER_SM:")
			// Print all fields
			for _, v := range pdu.MandatoryFieldsList() {
				f := pdu.GetField(v)
				fmt.Println("\t", v, ":", f)
			}
			// Respond back to Deliver SM with Deliver SM Resp
			err := trx.DeliverSmResp(pdu.GetHeader().Sequence, smpp.ESME_ROK)
			if err != nil {
				fmt.Println("DeliverSmResp err:", err)
			}
		case smpp.ENQUIRE_LINK_RESP: // ignore
		default:
			fmt.Println("PDU ID:", pdu.GetHeader().Id)
		}
	}
	fmt.Println("ending...")
}
