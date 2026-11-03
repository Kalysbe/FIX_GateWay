package arqa

import (
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func (a *sessionApp) handleFIXReject(msg *quickfix.Message) {
	text, _ := msg.Body.GetString(tag.Text)
	refType, _ := msg.Body.GetString(tag.RefMsgType)
	seq, _ := msg.Body.GetString(tag.RefSeqNum)
	ref, _ := msg.Body.GetString(tag.BusinessRejectRefID)
	if refType == "" {
		// 372 is optional in 35=3. Resolve the rejected message from our own
		// outbound history; an unknown reference stays a trading reject.
		if sent, ok := a.sentBySeq(seq); ok {
			refType = sent.msgType
			if ref == "" {
				ref = sent.clOrdID
			}
		}
	}
	if a.onExec != nil && (refType == "" || refType == "D" || refType == "F" || refType == "G") {
		a.onExec(domain.ExecutionReport{AdapterID: a.adapterID, ProtocolReject: true, RefMessageType: refType, RefSeqNum: seq, ClOrdID: ref, Text: decodeFIXText(text), RawFIX: decodeFIXText(formatWireFIX(msg))})
	} else if a.onExec != nil {
		a.log.Warn("fix reject of a non-trading message; trading not paused",
			"adapter_id", a.adapterID, "ref_msg_type", refType, "ref_seq_num", seq, "text", text)
	}
}
