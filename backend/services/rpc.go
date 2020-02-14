package services

import (
	"crawlab/constants"
	"crawlab/database"
	"crawlab/entity"
	"crawlab/model"
	"crawlab/utils"
	"encoding/json"
	"fmt"
	"github.com/apex/log"
	"github.com/gomodule/redigo/redis"
	uuid "github.com/satori/go.uuid"
	"runtime/debug"
)

type RpcMessage struct {
	Id     string            `json:"id"`
	Method string            `json:"method"`
	Params map[string]string `json:"params"`
	Result string            `json:"result"`
}

func RpcServerInstallLang(msg RpcMessage) RpcMessage {
	lang := GetRpcParam("lang", msg.Params)
	if lang == constants.Nodejs {
		output, _ := InstallNodejsLocalLang()
		msg.Result = output
	}
	return msg
}

func RpcClientInstallLang(nodeId string, lang string) (output string, err error) {
	params := map[string]string{}
	params["lang"] = lang

	data, err := RpcClientFunc(nodeId, constants.RpcInstallLang, params, 600)()
	if err != nil {
		return
	}

	output = data

	return
}

func RpcServerInstallDep(msg RpcMessage) RpcMessage {
	lang := GetRpcParam("lang", msg.Params)
	depName := GetRpcParam("dep_name", msg.Params)
	if lang == constants.Python {
		output, _ := InstallPythonLocalDep(depName)
		msg.Result = output
	}
	return msg
}

func RpcClientInstallDep(nodeId string, lang string, depName string) (output string, err error) {
	params := map[string]string{}
	params["lang"] = lang
	params["dep_name"] = depName

	data, err := RpcClientFunc(nodeId, constants.RpcInstallDep, params, 10)()
	if err != nil {
		return
	}

	output = data

	return
}

func RpcServerUninstallDep(msg RpcMessage) RpcMessage {
	lang := GetRpcParam("lang", msg.Params)
	depName := GetRpcParam("dep_name", msg.Params)
	if lang == constants.Python {
		output, _ := UninstallPythonLocalDep(depName)
		msg.Result = output
	}
	return msg
}

func RpcClientUninstallDep(nodeId string, lang string, depName string) (output string, err error) {
	params := map[string]string{}
	params["lang"] = lang
	params["dep_name"] = depName

	data, err := RpcClientFunc(nodeId, constants.RpcUninstallDep, params, 60)()
	if err != nil {
		return
	}

	output = data

	return
}

func RpcServerGetInstalledDepList(nodeId string, msg RpcMessage) RpcMessage {
	lang := GetRpcParam("lang", msg.Params)
	if lang == constants.Python {
		depList, _ := GetPythonLocalInstalledDepList(nodeId)
		resultStr, _ := json.Marshal(depList)
		msg.Result = string(resultStr)
	} else if lang == constants.Nodejs {
		depList, _ := GetNodejsLocalInstalledDepList(nodeId)
		resultStr, _ := json.Marshal(depList)
		msg.Result = string(resultStr)
	}
	return msg
}

func RpcClientGetInstalledDepList(nodeId string, lang string) (list []entity.Dependency, err error) {
	params := map[string]string{}
	params["lang"] = lang

	data, err := RpcClientFunc(nodeId, constants.RpcGetInstalledDepList, params, 10)()
	if err != nil {
		return
	}

	// 反序列化结果
	if err := json.Unmarshal([]byte(data), &list); err != nil {
		return list, err
	}

	return
}

func RpcClientFunc(nodeId string, method string, params map[string]string, timeout int) func() (string, error) {
	return func() (result string, err error) {
		// 请求ID
		id := uuid.NewV4().String()

		// 构造RPC消息
		msg := RpcMessage{
			Id:     id,
			Method: method,
			Params: params,
			Result: "",
		}

		// 发送RPC消息
		msgStr := ObjectToString(msg)
		if err := database.RedisClient.LPush(fmt.Sprintf("rpc:%s", nodeId), msgStr); err != nil {
			return result, err
		}

		// 获取RPC回复消息
		dataStr, err := database.RedisClient.BRPop(fmt.Sprintf("rpc:%s", nodeId), timeout)
		if err != nil {
			return result, err
		}

		// 反序列化消息
		if err := json.Unmarshal([]byte(dataStr), &msg); err != nil {
			return result, err
		}

		return msg.Result, err
	}
}

func GetRpcParam(key string, params map[string]string) string {
	return params[key]
}

func ObjectToString(params interface{}) string {
	bytes, _ := json.Marshal(params)
	return utils.BytesToString(bytes)
}

var IsRpcStopped = false

func StopRpcService() {
	IsRpcStopped = true
}

func InitRpcService() error {
	go func() {
		for {
			// 获取当前节点
			node, err := model.GetCurrentNode()
			if err != nil {
				log.Errorf(err.Error())
				debug.PrintStack()
				continue
			}

			// 获取获取消息队列信息
			dataStr, err := database.RedisClient.BRPop(fmt.Sprintf("rpc:%s", node.Id.Hex()), 0)
			if err != nil {
				if err != redis.ErrNil {
					log.Errorf(err.Error())
					debug.PrintStack()
				}
				continue
			}

			// 反序列化消息
			var msg RpcMessage
			if err := json.Unmarshal([]byte(dataStr), &msg); err != nil {
				log.Errorf(err.Error())
				debug.PrintStack()
				continue
			}

			// 根据Method调用本地方法
			var replyMsg RpcMessage
			if msg.Method == constants.RpcInstallDep {
				replyMsg = RpcServerInstallDep(msg)
			} else if msg.Method == constants.RpcUninstallDep {
				replyMsg = RpcServerUninstallDep(msg)
			} else if msg.Method == constants.RpcInstallLang {
				replyMsg = RpcServerInstallLang(msg)
			} else if msg.Method == constants.RpcGetInstalledDepList {
				replyMsg = RpcServerGetInstalledDepList(node.Id.Hex(), msg)
			} else {
				continue
			}

			// 发送返回消息
			if err := database.RedisClient.LPush(fmt.Sprintf("rpc:%s", node.Id.Hex()), ObjectToString(replyMsg)); err != nil {
				log.Errorf(err.Error())
				debug.PrintStack()
				continue
			}

			// 如果停止RPC服务，则返回
			if IsRpcStopped {
				return
			}
		}
	}()
	return nil
}
