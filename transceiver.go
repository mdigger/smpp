package smpp

import (
	"crypto/tls"
	"time"
)

type Transceiver struct {
	Smpp
	eLTicker     *time.Ticker  // Enquire Link ticker
	eLCheckTimer *time.Timer   // Enquire Link Check timer
	eLDuration   time.Duration // Enquire Link Duration
	Err          error         // Errors generated in go routines that lead to conn close
}

// NewTransceiver creates and initializes a new Transceiver.
// The eli parameter is for EnquireLink interval, in seconds.
func NewTransceiver(addr string, eli time.Duration, bindParams Params) (*Transceiver, error) {
	return newTransceiver(addr, eli, bindParams, nil)
}

// NewTransceiver creates and initializes a new Transceiver using TLS.
// The eli parameter is for EnquireLink interval, in seconds.
func NewTransceiverTLS(addr string, eli time.Duration, bindParams Params, config *tls.Config) (*Transceiver, error) {
	if config == nil {
		config = &tls.Config{}
	}
	return newTransceiver(addr, eli, bindParams, config)
}

// eli = EnquireLink Interval in Seconds
func newTransceiver(addr string, eli time.Duration, bindParams Params, config *tls.Config) (trx *Transceiver, err error) {
	trx = &Transceiver{}
	if config == nil {
		err = trx.Connect(addr)
	} else {
		err = trx.ConnectTLS(addr, config)
	}
	if err != nil {
		return nil, err
	}
	sysId := bindParams[SYSTEM_ID].(string)
	pass := bindParams[PASSWORD].(string)
	if err := trx.Bind(sysId, pass, &bindParams); err != nil {
		return nil, err
	}
	// EnquireLinks should not be less 10seconds
	if eli < time.Second*10 {
		eli = time.Second * 10
	}
	trx.eLDuration = eli
	go trx.startEnquireLink(eli)
	return trx, nil
}

func (t *Transceiver) Bind(system_id string, password string, params *Params) error {
	pdu, err := t.Smpp.Bind(BIND_TRANSCEIVER, system_id, password, params)
	if err := t.Write(pdu); err != nil {
		return err
	}
	// If BindResp NOT received in 5secs close connection
	go t.bindCheck()
	// Read (blocking)
	pdu, err = t.Smpp.Read()
	if err != nil {
		return err
	}
	if pdu.GetHeader().Id != BIND_TRANSCEIVER_RESP {
		return SmppBindRespErr
	}
	if !pdu.Ok() {
		return SmppBindAuthErr("Bind auth failed. " + pdu.GetHeader().Status.Error())
	}
	t.Bound = true
	return nil
}

func (t *Transceiver) SubmitSm(source_addr, destination_addr, short_message string, params Params) (seq uint32, err error) {
	p, err := t.Smpp.SubmitSm(source_addr, destination_addr, short_message, params)
	if err != nil {
		return 0, err
	}
	if err := t.Write(p); err != nil {
		return 0, err
	}
	return p.GetHeader().Sequence, nil
}

func (t *Transceiver) DeliverSmResp(seq uint32, status CMDStatus) error {
	p, err := t.Smpp.DeliverSmResp(seq, status)
	if err != nil {
		return err
	}
	return t.Write(p)
}

func (t *Transceiver) Unbind() error {
	p, _ := t.Smpp.Unbind()
	return t.Write(p)
}

func (t *Transceiver) UnbindResp(seq uint32) error {
	p, _ := t.Smpp.UnbindResp(seq)
	if err := t.Write(p); err != nil {
		return err
	}
	t.Bound = false
	return nil
}

func (t *Transceiver) GenericNack(seq uint32, status CMDStatus) error {
	p, _ := t.Smpp.GenericNack(seq, status)
	return t.Write(p)
}

func (t *Transceiver) bindCheck() {
	// Block
	time.Sleep(5 * time.Second)
	if !t.Bound {
		// send error to t.err? So it can be read before closing.
		t.Err = SmppBindRespErr
		t.Close()
	}
}

func (t *Transceiver) startEnquireLink(eli time.Duration) {
	t.eLTicker = time.NewTicker(eli)
	// check delay is half the time of enquire link intervel
	d := time.Duration(eli / 2)
	t.eLCheckTimer = time.NewTimer(d)
	t.eLCheckTimer.Stop()
	for {
		select {
		case <-t.eLTicker.C:
			p, _ := t.EnquireLink()
			if err := t.Write(p); err != nil {
				t.Err = SmppELWriteErr
				t.Close()
				return
			}
			t.eLCheckTimer.Reset(d)
		case <-t.eLCheckTimer.C:
			t.Err = SmppELRespErr
			t.Close()
			return
		}
	}
}

func (t *Transceiver) Read() (Pdu, error) {
	pdu, err := t.Smpp.Read()
	if err != nil {
		if _, ok := err.(PduCmdIdErr); ok {
			// Invalid PDU Command ID, should send back GenericNack
			t.GenericNack(uint32(0), ESME_RINVCMDID)
		} else if SmppPduLenErr == err {
			// Invalid PDU, PDU read or Len error
			t.GenericNack(uint32(0), ESME_RINVCMDLEN)
		}
		return nil, err
	}

	switch pdu.GetHeader().Id {
	case SUBMIT_SM_RESP, DELIVER_SM:
		break
	case ENQUIRE_LINK:
		p, _ := t.Smpp.EnquireLinkResp(pdu.GetHeader().Sequence)
		if err := t.Write(p); err != nil {
			return nil, err
		}
	case ENQUIRE_LINK_RESP:
		// Reset EnquireLink Check
		t.eLCheckTimer.Stop()
	case UNBIND:
		t.UnbindResp(pdu.GetHeader().Sequence)
		t.Close()
	default:
		// Should not have received these PDUs on a TRx bind
		return nil, SmppPduErr
	}

	return pdu, nil
}

func (t *Transceiver) Close() error {
	// Check timers exists incase we Close() before timers are created
	if t.eLCheckTimer != nil {
		t.eLCheckTimer.Stop()
	}
	if t.eLTicker != nil {
		t.eLTicker.Stop()
	}
	return t.Smpp.Close()
}

func (t *Transceiver) Write(p Pdu) error {
	return t.Smpp.Write(p)
}
