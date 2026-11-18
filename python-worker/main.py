import jsonrpclib

server = jsonrpclib.Server("http://127.0.0.1:8001")
print(server.Service.TestRPC({"a": 1, "b": 2}))
