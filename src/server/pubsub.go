package server

import (
	pb_systemmanager_messages "github.com/VU-ASE/pkg-CommunicationDefinitions/v2/packages/go/systemmanager"
	zmq "github.com/pebbe/zmq4"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

// Sets up a socket that can be used to broadcast messages to all services
func SetupBroadcast(address string) (*zmq.Socket, error) {
	publisher, err := zmq.NewSocket(zmq.PUB)
	if err != nil {
		return nil, err
	}
	err = publisher.Bind(address)
	return publisher, err
}

func BroadcastMessage(publisher *zmq.Socket, message *pb_systemmanager_messages.SystemManagerMessage) error {
	if publisher == nil {
		log.Warn().Msg("Was asked to broadcast a message, but no publisher was set up. Ignoring.")
		return nil
	}

	// Marshal the message
	messageBytes, err := proto.Marshal(message)
	if err != nil {
		return err
	}

	_, err = publisher.SendBytes(messageBytes, 0)
	return err
}
