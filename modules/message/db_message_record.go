package message

type messageRecord struct {
	ID          int64  `db:"id" json:"id"`
	MessageID   string `db:"message_id" json:"message_id"`
	MessageSeq  int64  `db:"message_seq" json:"message_seq"`
	ClientMsgNo string `db:"client_msg_no" json:"client_msg_no"`
	Setting     int16  `db:"setting" json:"setting"`
	Signal      int16  `db:"signal" json:"signal"`
	Header      string `db:"header" json:"header"`
	FromUID     string `db:"from_uid" json:"from_uid"`
	ChannelID   string `db:"channel_id" json:"channel_id"`
	ChannelType int16  `db:"channel_type" json:"channel_type"`
	ContentType int16  `db:"content_type" json:"content_type"`
	Payload     []byte `db:"payload" json:"payload"`
	CTS         int64  `db:"cts" json:"cts"`
}
