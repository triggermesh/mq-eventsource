// Copyright 2020 TriggerMesh, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"log"
	"sync"

	"github.com/caarlos0/env"
	"github.com/cloudevents/sdk-go/pkg/cloudevents"
	"github.com/cloudevents/sdk-go/pkg/cloudevents/types"
	"github.com/ibm-messaging/mq-golang/ibmmq"
	"knative.dev/eventing-contrib/pkg/kncloudevents"
)

var sink string

type config struct {
	QueueManager   string `env:"QUEUE_MANAGER" envDefault:"QM1"`
	ChannelName    string `env:"CHANNEL_NAME" envDefault:"DEV.APP.SVRCONN"`
	ConnectionName string `env:"CONNECTION_NAME" envDefault:"localhost(1414)"`
	UserID         string `env:"USER_ID" envDefault:"app"`
	Password       string `env:"PASSWORD" envDefault:"password"`
	QueueName      string `env:"QUEUE_NAME" envDefault:"DEV.QUEUE.1"`
	EventType      string `env:"EVENT_TYPE" envDefault:"dev.triggermesh.eventing.ibm-mq"`
}

func init() {
	flag.StringVar(&sink, "sink", "http://localhost:8080", "where to sink events to")
}

func main() {
	flag.Parse()

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(err)
	}

	cloudEventsClient, err := kncloudevents.NewDefaultClient(sink)
	if err != nil {
		log.Fatalf("failed to create client: %s", err.Error())
	}

	// create IBM MQ channel definition
	channelDefinition := ibmmq.NewMQCD()
	channelDefinition.ChannelName = cfg.ChannelName
	channelDefinition.ConnectionName = cfg.ConnectionName

	// init connection security params
	connSecParams := ibmmq.NewMQCSP()
	connSecParams.AuthenticationType = ibmmq.MQCSP_AUTH_USER_ID_AND_PWD
	connSecParams.UserId = cfg.UserID
	connSecParams.Password = cfg.Password

	// setup MQ connection params
	connOptions := ibmmq.NewMQCNO()
	connOptions.Options = ibmmq.MQCNO_CLIENT_BINDING
	connOptions.Options |= ibmmq.MQCNO_HANDLE_SHARE_BLOCK
	connOptions.ClientConn = channelDefinition
	connOptions.SecurityParms = connSecParams

	// And now we can try to connect. Wait a short time before disconnecting.
	qMgrObject, err := ibmmq.Connx(cfg.QueueManager, connOptions)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Connection to %q succeeded.\n", cfg.QueueManager)
	defer disconnect(qMgrObject)

	// Create the Object Descriptor that allows us to give the queue name
	mqod := ibmmq.NewMQOD()

	// We have to say how we are going to use this queue. In this case, to GET
	// messages. That is done in the openOptions parameter.
	openOptions := ibmmq.MQOO_INPUT_SHARED

	// Opening a QUEUE (rather than a Topic or other object type) and give the name
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = cfg.QueueName

	qObject, err := qMgrObject.Open(mqod, openOptions)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Opened queue", qObject.Name)
	var wg sync.WaitGroup

	defer close(qObject)
	defer wg.Wait()

	for {
		// The GET requires control structures, the Message Descriptor (MQMD)
		// and Get Options (MQGMO). Create those with default values.
		msgDescriptor := ibmmq.NewMQMD()
		msgOptions := ibmmq.NewMQGMO()

		// The default options are OK, but it's always
		// a good idea to be explicit about transactional boundaries as
		// not all platforms behave the same way.
		msgOptions.Options = ibmmq.MQGMO_SYNCPOINT
		msgOptions.Options |= ibmmq.MQGMO_WAIT
		// Wait interval is 10 seconds
		// Not setting "MQWI_UNLIMITED" to retry backed out messages
		msgOptions.WaitInterval = 10 * 1000

		// Create a buffer for the message data. This one is large enough
		// for the messages put by the amqsput sample.
		buffer := make([]byte, 1024)

		// Now we can try to get the message
		_, _, err := qObject.GetSlice(msgDescriptor, msgOptions, buffer)
		if err != nil {
			mqret := err.(*ibmmq.MQReturn)
			if mqret != nil && mqret.MQRC == ibmmq.MQRC_NO_MSG_AVAILABLE {
				continue
			}
			log.Printf("Error retrieving message: %s\n", err)
			break
		}

		wg.Add(1)
		go func(md *ibmmq.MQMD, messageData []byte) {
			defer wg.Done()

			event := cloudevents.Event{
				Context: cloudevents.EventContextV1{
					Type:   cfg.EventType,
					Source: *types.ParseURIRef(cfg.ConnectionName),
				}.AsV1(),
				Data: messageData,
			}

			if _, _, err := cloudEventsClient.Send(context.Background(), event); err != nil {
				log.Printf("Message send backout, %d: %v\n", md.BackoutCount, err)
				if err := qMgrObject.Back(); err != nil {
					log.Printf("Can't backout failed transaction: %v\n", err)
				}
			} else {
				if err := qMgrObject.Cmit(); err != nil {
					log.Printf("Can't commit transaction: %v\n", err)
				}
			}
		}(msgDescriptor, buffer)
	}
}

// Disconnect from the queue manager
func disconnect(qMgrObject ibmmq.MQQueueManager) {
	if err := qMgrObject.Disc(); err != nil {
		log.Fatal(err)
	}
}

// Close the queue if it was opened
func close(object ibmmq.MQObject) {
	if err := object.Close(0); err != nil {
		log.Fatal(err)
	}
}
