// Copyright (c) IBM Corporation 2019.
//
// This program and the accompanying materials are made available under the
// terms of the Eclipse Public License 2.0, which is available at
// http://www.eclipse.org/legal/epl-2.0.
//
// SPDX-License-Identifier: EPL-2.0

// package mqibm provides the implementation of the JMS style Golang interfaces to communicate with IBM MQ.
package mqibm

import (
	"errors"
	"strconv"
	"strings"
)

// ConsumerImpl defines a struct that contains the necessary objects for
// receiving messages from a queue on an IBM MQ queue manager.
type ConsumerImpl struct {
	ctx      ContextImpl
	qObject  MQObject
	selector string
}

// ReceiveNoWait implements the IBM MQ logic necessary to receive a message from
// a Destination, or immediately return a nil Message if there is no available
// message to be received.
func (consumer ConsumerImpl) ReceiveNoWait() (Message, JMSException) {

	gmo := NewMQGMO()
	return consumer.receiveInternal(gmo)

}

// Receive with waitMillis returns a message if one is available, or otherwise
// waits for up to the specified number of milliseconds for one to become
// available. A value of zero or less indicates to wait indefinitely.
func (consumer ConsumerImpl) Receive(waitMillis int32) (Message, JMSException) {

	if waitMillis <= 0 {
		waitMillis = MQWI_UNLIMITED
	}

	gmo := NewMQGMO()
	gmo.Options |= MQGMO_WAIT
	gmo.WaitInterval = waitMillis

	return consumer.receiveInternal(gmo)

}

// Internal method to provide common functionality across the different types
// of receive.
func (consumer ConsumerImpl) receiveInternal(gmo *MQGMO) (Message, JMSException) {

	// Prepare objects to be used in receiving the message.
	var msg Message
	var jmsErr JMSException

	getmqmd := NewMQMD()

	myBufferSize := 32768

	if consumer.ctx.receiveBufferSize > 0 {
		myBufferSize = consumer.ctx.receiveBufferSize
	}

	buffer := make([]byte, myBufferSize)

	// Calculate the syncpoint value
	syncpointSetting := MQGMO_NO_SYNCPOINT
	if consumer.ctx.sessionMode == JMSContextSESSIONTRANSACTED {
		syncpointSetting = MQGMO_SYNCPOINT
	}

	// Set the GMO (get message options)
	gmo.Options |= syncpointSetting
	gmo.Options |= MQGMO_FAIL_IF_QUIESCING

	// Include the message properties in the msgHandle
	gmo.Options |= MQGMO_PROPERTIES_IN_HANDLE
	cmho := NewMQCMHO()
	thisMsgHandle, _ := consumer.ctx.qMgr.CrtMH(cmho)
	gmo.MsgHandle = thisMsgHandle

	// Apply the selector if one has been specified in the Consumer
	err := applySelector(consumer.selector, getmqmd, gmo)
	if err != nil {
		jmsErr = CreateJMSException("ErrorParsingSelector", "ErrorParsingSelector", err)
		return nil, jmsErr
	}

	// Use the prepared objects to ask for a message from the queue.
	datalen, err := consumer.qObject.Get(getmqmd, gmo, buffer)

	if err == nil {

		// Message received successfully (without error).
		// Determine on the basis of the format field what sort of message to create.

		if getmqmd.Format == MQFMT_STRING {

			var msgBodyStr *string

			if datalen > 0 {
				strContent := string(buffer[:datalen])
				msgBodyStr = &strContent
			}

			msg = &TextMessageImpl{
				bodyStr: msgBodyStr,
				MessageImpl: MessageImpl{
					mqmd:      getmqmd,
					msgHandle: &thisMsgHandle,
				},
			}

		} else {

			if datalen == 0 {
				buffer = []byte{}
			}

			trimmedBuffer := buffer[0:datalen]

			// Not a string, so fall back to BytesMessage
			msg = &BytesMessageImpl{
				bodyBytes: &trimmedBuffer,
				MessageImpl: MessageImpl{
					mqmd:      getmqmd,
					msgHandle: &thisMsgHandle,
				},
			}
		}

	} else {

		// Error code was returned from MQ call.
		mqret := err.(*MQReturn)

		if mqret.MQRC == MQRC_NO_MSG_AVAILABLE {

			// This isn't a real error - it's the way that MQ indicates that there
			// is no message available to be received.
			msg = nil

		} else {

			// Parse the details of the error and return it to the caller as
			// a JMSException
			rcInt := int(mqret.MQRC)
			errCode := strconv.Itoa(rcInt)
			reason := MQItoString("RC", rcInt)

			jmsErr = CreateJMSException(reason, errCode, err)
		}

	}

	return msg, jmsErr
}

// ReceiveStringBodyNoWait implements the IBM MQ logic necessary to receive a
// message from a Destination and return its body as a string.
//
// If no message is immediately available to be returned then a nil is returned.
func (consumer ConsumerImpl) ReceiveStringBodyNoWait() (*string, JMSException) {

	var msgBodyStrPtr *string
	var jmsErr JMSException

	// Get a message from the queue if one is available.
	msg, jmsErr := consumer.ReceiveNoWait()

	// If we receive a message without any errors
	if jmsErr == nil && msg != nil {

		switch msg := msg.(type) {
		case TextMessage:
			msgBodyStrPtr = msg.GetText()
		default:
			jmsErr = CreateJMSException(
				"MQJMS_DIR_MIN_NOTTEXT", "MQJMS6068", nil)
		}

	}

	return msgBodyStrPtr, jmsErr

}

// ReceiveStringBody implements the IBM MQ logic necessary to receive a
// message from a Destination and return its body as a string.
//
// If no message is available the method blocks up to the specified number
// of milliseconds for one to become available.
func (consumer ConsumerImpl) ReceiveStringBody(waitMillis int32) (*string, JMSException) {

	var msgBodyStrPtr *string
	var jmsErr JMSException

	// Get a message from the queue if one is available.
	msg, jmsErr := consumer.Receive(waitMillis)

	// If we receive a message without any errors
	if jmsErr == nil && msg != nil {

		switch msg := msg.(type) {
		case TextMessage:
			msgBodyStrPtr = msg.GetText()
		default:
			jmsErr = CreateJMSException(
				"MQJMS_DIR_MIN_NOTTEXT", "MQJMS6068", nil)
		}

	}

	return msgBodyStrPtr, jmsErr

}

// ReceiveBytesBodyNoWait implements the IBM MQ logic necessary to receive a
// message from a Destination and return its body as a slice of bytes.
//
// If no message is immediately available to be returned then a nil is returned.
func (consumer ConsumerImpl) ReceiveBytesBodyNoWait() (*[]byte, JMSException) {

	var msgBodyPtr *[]byte
	var jmsErr JMSException

	// Get a message from the queue if one is available.
	msg, jmsErr := consumer.ReceiveNoWait()

	// If we receive a message without any errors
	if jmsErr == nil && msg != nil {

		switch msg := msg.(type) {
		case BytesMessage:
			msgBodyPtr = msg.ReadBytes()
		default:
			jmsErr = CreateJMSException(
				"MQJMS_DIR_MIN_NOTBYTES", "MQJMS6068", nil)
		}

	}

	return msgBodyPtr, jmsErr

}

// ReceiveBytesBody implements the IBM MQ logic necessary to receive a
// message from a Destination and return its body as a slice of bytes.
//
// If no message is available the method blocks up to the specified number
// of milliseconds for one to become available.
func (consumer ConsumerImpl) ReceiveBytesBody(waitMillis int32) (*[]byte, JMSException) {

	var msgBodyPtr *[]byte
	var jmsErr JMSException

	// Get a message from the queue if one is available.
	msg, jmsErr := consumer.Receive(waitMillis)

	// If we receive a message without any errors
	if jmsErr == nil && msg != nil {

		switch msg := msg.(type) {
		case BytesMessage:
			msgBodyPtr = msg.ReadBytes()
		default:
			jmsErr = CreateJMSException(
				"MQJMS_DIR_MIN_NOTBYTES", "MQJMS6068", nil)
		}

	}

	return msgBodyPtr, jmsErr

}

// applySelector is responsible for converting the JMS style selector string
// into the relevant options on the MQI structures so that the correct messages
// are received by the application.
func applySelector(selector string, getmqmd *MQMD, gmo *MQGMO) error {

	if selector == "" {
		// No selector is provided, so nothing to do here.
		return nil
	}

	// looking for something like
	//   "JMSCorrelationID = '01020304050607'"
	//   "JMSMessageID = '414d5120514d31202020202020202020bec0ba61034dbe40'"
	clauseSplits := strings.Split(selector, "=")

	if len(clauseSplits) != 2 {
		return errors.New("Unable to parse selector " + selector)
	}

	selectorFieldName := strings.TrimSpace(clauseSplits[0])

	if selectorFieldName != "JMSCorrelationID" &&
		selectorFieldName != "JMSMessageID" {

		// Currently we only support correlID and messageID selectors, so error out quickly
		// if we see anything else.
		return errors.New("Only selectors on JMSCorrelationID and JMSMessageID are currently supported")
	}

	// Trim the value.
	value := strings.TrimSpace(clauseSplits[1])

	// Check for a quote delimited value for the selector clause.
	if strings.HasPrefix(value, "'") &&
		strings.HasSuffix(value, "'") {

		// Parse out the value, and convert it to bytes
		stringSplits := strings.Split(value, "'")
		selectorValue := stringSplits[1]

		// For CorrelID and MsgID there is typically an "ID:" prefix on the
		// selector value that needs to be trimmed off before we convert it.
		if strings.HasPrefix(selectorValue, "ID:") {
			selectorValue = selectorValue[3:]
		}

		if selectorValue != "" {

			selectorValueBytes := convertStringToMQBytes(selectorValue)

			switch selectorFieldName {
			case "JMSCorrelationID":
				getmqmd.CorrelId = selectorValueBytes

			case "JMSMessageID":
				getmqmd.MsgId = selectorValueBytes
			}

		} else {
			return errors.New("No value was found for selector string")
		}

	} else {
		return errors.New("Unable to parse quoted string from " + selector)
	}

	return nil
}

// Close closes the JMSConsumer, releasing any resources that were allocated on
// behalf of that consumer.
func (consumer ConsumerImpl) Close() {

	if (MQObject{}) != consumer.qObject {
		consumer.qObject.Close(0)
	}

	return
}