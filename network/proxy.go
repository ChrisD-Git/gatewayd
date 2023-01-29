package network

import (
	"errors"
	"fmt"
	"io"

	"github.com/panjf2000/gnet/v2"
	"github.com/rs/zerolog"
)

type Traffic func(gconn gnet.Conn, cl *Client, buf []byte, err error) error

type Proxy interface {
	Connect(gconn gnet.Conn) error
	Disconnect(gconn gnet.Conn) error
	PassThrough(gconn gnet.Conn, onIncomingTraffic, onOutgoingTraffic map[Prio]Traffic) error
	Reconnect(cl *Client) *Client
	Shutdown()
}

type ProxyImpl struct {
	availableConnections Pool
	busyConnections      Pool
	logger               zerolog.Logger

	Elastic             bool
	ReuseElasticClients bool

	// ClientConfig is used for elastic proxy and reconnection
	ClientConfig *Client
}

var _ Proxy = &ProxyImpl{}

func NewProxy(
	pool Pool, elastic, reuseElasticClients bool, clientConfig *Client, logger zerolog.Logger,
) *ProxyImpl {
	return &ProxyImpl{
		availableConnections: pool,
		busyConnections:      NewEmptyPool(logger),
		logger:               logger,
		Elastic:              elastic,
		ReuseElasticClients:  reuseElasticClients,
		ClientConfig:         clientConfig,
	}
}

func (pr *ProxyImpl) Connect(gconn gnet.Conn) error {
	var clientIDs []string
	pr.availableConnections.ForEach(func(key, _ interface{}) bool {
		if clientID, ok := key.(string); ok {
			clientIDs = append(clientIDs, clientID)
		}
		return true
	})

	var client *Client
	if len(clientIDs) == 0 {
		// Pool is exhausted
		if pr.Elastic {
			// Create a new client
			client = NewClient(
				pr.ClientConfig.Network,
				pr.ClientConfig.Address,
				pr.ClientConfig.ReceiveBufferSize,
				pr.logger,
			)
			pr.logger.Debug().Msgf("Reused the client %s by putting it back in the pool", client.ID)
		} else {
			return ErrPoolExhausted
		}
	} else {
		// Get a client from the pool
		pr.logger.Debug().Msgf("Available clients: %v", len(clientIDs))
		if cl, ok := pr.availableConnections.Pop(clientIDs[0]).(*Client); ok {
			client = cl
		}
	}

	if client.ID != "" {
		pr.busyConnections.Put(gconn, client)
		pr.logger.Debug().Msgf("Client %s has been assigned to %s", client.ID, gconn.RemoteAddr().String())
	} else {
		return ErrClientNotConnected
	}

	pr.logger.Debug().Msgf("[C] There are %d clients in the pool", pr.availableConnections.Size())
	pr.logger.Debug().Msgf("[C] There are %d clients in use", pr.busyConnections.Size())

	return nil
}

func (pr *ProxyImpl) Disconnect(gconn gnet.Conn) error {
	var client *Client
	if cl, ok := pr.busyConnections.Pop(gconn).(*Client); !ok {
		client = cl
	}

	// TODO: The connection is unstable when I put the client back in the pool
	// If the client is not in the pool, put it back
	if pr.Elastic && pr.ReuseElasticClients || !pr.Elastic {
		client = pr.Reconnect(client)
		if client != nil && client.ID != "" {
			pr.availableConnections.Put(client.ID, client)
		}
	} else {
		client.Close()
	}

	pr.logger.Debug().Msgf("[D] There are %d clients in the pool", pr.availableConnections.Size())
	pr.logger.Debug().Msgf("[D] There are %d clients in use", pr.busyConnections.Size())

	return nil
}

//nolint:funlen
func (pr *ProxyImpl) PassThrough(
	gconn gnet.Conn,
	onIncomingTraffic,
	onOutgoingTraffic map[Prio]Traffic,
) error {
	// TODO: Handle bi-directional traffic
	// Currently the passthrough is a one-way street from the client to the server, that is,
	// the client can send data to the server and receive the response back, but the server
	// cannot take initiative and send data to the client. So, there should be another event-loop
	// that listens for data from the server and sends it to the client

	var client *Client
	if cl, ok := pr.busyConnections.Pop(gconn).(*Client); ok {
		client = cl
	} else {
		return ErrClientNotFound
	}

	// buf contains the data from the client (query)
	buf, err := gconn.Next(-1)
	if err != nil {
		pr.logger.Error().Err(err).Msgf("Error reading from client: %v", err)
	}
	for _, traffic := range onIncomingTraffic {
		if err = traffic(gconn, client, buf, err); err != nil {
			pr.logger.Error().Err(err).Msgf("Error processing data from client: %v", err)
		}
	}

	// TODO: This is a very basic implementation of the gateway
	// and it is synchronous. I should make it asynchronous.
	pr.logger.Debug().Msgf("Received %d bytes from %s", len(buf), gconn.RemoteAddr().String())

	// Send the query to the server
	err = client.Send(buf)
	if err != nil {
		return err
	}

	// Receive the response from the server
	size, response, err := client.Receive()
	for _, traffic := range onOutgoingTraffic {
		if err := traffic(gconn, client, response[:size], err); err != nil {
			pr.logger.Error().Err(err).Msgf("Error processing data from server: %s", err)
		}
	}

	if err != nil && errors.Is(err, io.EOF) {
		// The server has closed the connection
		pr.logger.Error().Err(err).Msg("The client is not connected to the server anymore")
		// Either the client is not connected to the server anymore or
		// server forceful closed the connection
		// Reconnect the client
		client = pr.Reconnect(client)
		// Put the client in the busy connections pool, effectively replacing the old one
		pr.busyConnections.Put(gconn, client)
		return err
	}

	if err != nil {
		// Write the error to the client
		_, err := gconn.Write(response[:size])
		if err != nil {
			pr.logger.Error().Err(err).Msgf("Error writing the error to client: %v", err)
		}
		return fmt.Errorf("error receiving data from server: %w", err)
	}

	// Write the response to the incoming connection
	_, err = gconn.Write(response[:size])
	if err != nil {
		pr.logger.Error().Err(err).Msgf("Error writing to client: %v", err)
	}

	return nil
}

func (pr *ProxyImpl) Reconnect(cl *Client) *Client {
	// Close the client
	if cl != nil && cl.ID != "" {
		cl.Close()
	}
	return NewClient(
		pr.ClientConfig.Network,
		pr.ClientConfig.Address,
		pr.ClientConfig.ReceiveBufferSize,
		pr.logger,
	)
}

func (pr *ProxyImpl) Shutdown() {
	pr.availableConnections.ForEach(func(key, value interface{}) bool {
		if cl, ok := value.(*Client); ok {
			cl.Close()
		}
		return true
	})
	pr.availableConnections.Clear()
	pr.logger.Debug().Msg("All available connections have been closed")

	pr.busyConnections.ForEach(func(key, value interface{}) bool {
		if gconn, ok := key.(gnet.Conn); ok {
			gconn.Close()
		}
		if cl, ok := value.(*Client); ok {
			cl.Close()
		}
		return true
	})
	pr.busyConnections.Clear()
	pr.logger.Debug().Msg("All busy connections have been closed")
}
