package message

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type Http struct {
	Received chan string  // received messages from web
	ToSend   chan Message // messages to send to web
}

type Req struct {
	Message string `json:"message" form:"message"`
}

func (p *Http) GetHandle(c *gin.Context) {
	select {
	case message := <-p.ToSend:
		c.JSON(200, gin.H{
			"code":    200,
			"message": message,
			"data":    "",
		})
	default:
		c.JSON(200, gin.H{
			"code":    404,
			"message": "",
			"data":    "",
		})
		c.Abort()
	}
}

func (p *Http) SendHandle(c *gin.Context) {
	var req Req
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(200, gin.H{
			"code":    400,
			"message": err,
			"data":    "",
		})
		c.Abort()
		return
	}
	select {
	case p.Received <- req.Message:
		c.JSON(200, gin.H{
			"code":    200,
			"message": "",
			"data":    "",
		})
	default:
		c.JSON(200, gin.H{
			"code":    500,
			"message": "nowhere needed",
			"data":    "",
		})
		c.Abort()
	}
}

func (p *Http) Send(message Message) error {
	select {
	case p.ToSend <- message:
		return nil
	default:
		return errors.New("send failed")
	}
}

func (p *Http) Receive() (string, error) {
	select {
	case message := <-p.Received:
		return message, nil
	default:
		return "", errors.New("receive failed")
	}
}

func (p *Http) WaitSend(message Message, d int) error {
	select {
	case p.ToSend <- message:
		return nil
	case <-time.After(time.Duration(d) * time.Second):
		return errors.New("send timeout")
	}
}

func (p *Http) WaitReceive(d int) (string, error) {
	select {
	case message := <-p.Received:
		return message, nil
	case <-time.After(time.Duration(d) * time.Second):
		return "", errors.New("receive timeout")
	}
}

var HttpInstance = &Http{
	Received: make(chan string),
	ToSend:   make(chan Message),
}