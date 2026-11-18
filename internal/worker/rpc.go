package worker

import "log"

type Service struct{}

type Request struct {
	A int `json:"a"`
	B int `json:"b"`
}

type Response struct {
	Res int `json:"res"`
}

func (s *Service) TestRPC(req Request, res *Response) error {
	log.Println("Client Request for TestRPC")
	res.Res = req.A + req.B
	return nil
}
