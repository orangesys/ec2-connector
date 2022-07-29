/*
Copyright © 2020 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2instanceconnect"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/orangesys/ec2-connector/pkg/loki"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect [instance ID]",
	Short: "Connect instances.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID := args[0]

		mySession := session.Must(session.NewSession())
		// get user arn
		_sts := sts.New(mySession, aws.NewConfig().WithRegion("ap-northeast-1"))
		userInfo, err := _sts.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		userArn := *userInfo.Arn

		svc := ec2.New(mySession, aws.NewConfig().WithRegion("ap-northeast-1"))
		out, err := svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: []*string{
				aws.String(args[0]),
			},
		})
		if err != nil {
			return err
		}
		instance := out.Reservations[0].Instances[0]

		privKey, pubKey, err := generateSSHKeypair()
		if err != nil {
			return err
		}
		if err := sendSSHPublicKey(instanceID, string(pubKey)); err != nil {
			return err
		}
		config := &ssh.ClientConfig{
			User:            "ubuntu",
			Auth:            []ssh.AuthMethod{keyAuth(privKey)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}

		sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", *(instance.PublicIpAddress)), config)
		if err != nil {
			return err
		}
		defer sshClient.Close()

		session, err := sshClient.NewSession()
		if err != nil {
			return err
		}
		defer session.Close()

		session.Stdout = os.Stdout
		session.Stderr = os.Stderr
		in, err := session.StdinPipe()
		defer in.Close()

		buf := bytes.NewBufferString("")

		go func() {
			for {
				var buffer [1024]byte
				n, err := os.Stdin.Read(buffer[:])
				if err != nil {
					fmt.Println("read error:", err)
				}
				in.Write(buffer[:n])
				// Loki log
				buf.WriteString(string(buffer[:n]))

				if strings.Contains(buf.String(), "\u007f") {
					str := buf.String()
					buf.Reset()
					if len(str) > 2 {
						buf.WriteString(str[:len(str)-2])
					} else {
						buf.WriteString(str[:len(str)-1])
					}
				}
				if strings.Contains(buf.String(), "\r") {
					var logger = loki.NewLog()
					cmd := strings.Replace(buf.String(), "\r", "", -1)
					logger.Info().Str("cmd", cmd).Str("arn", userArn).Msg("user operator record")
					buf.Reset()
				}

			}
		}()

		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}

		fileDescriptor := int(os.Stdin.Fd())

		if terminal.IsTerminal(fileDescriptor) {
			originalState, err := terminal.MakeRaw(fileDescriptor)
			if err != nil {
			}
			defer terminal.Restore(fileDescriptor, originalState)

			termWidth, termHeight, err := terminal.GetSize(fileDescriptor)
			if err != nil {
			}

			err = session.RequestPty("xterm-256color", termHeight, termWidth, modes)
			if err != nil {
			}
		}
		if err = session.Shell(); err != nil {
			return errors.Wrap(err, fmt.Sprintf("start shell error: %s", err.Error()))
		}
		if err = session.Wait(); err != nil {
			return errors.Wrap(err, fmt.Sprintf("return error: %s", err.Error()))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// connectCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// connectCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func generateSSHKeypair() ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.Wrap(err, "generating new ssh private key")
	}

	buf := bytes.Buffer{}
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	err = pem.Encode(&buf, privateKeyPEM)
	if err != nil {
		return nil, nil, errors.Wrap(err, "pem-encoding new ssh private key")
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "creating signer from ssh priv key")
	}

	public := ssh.MarshalAuthorizedKey(signer.PublicKey())
	return buf.Bytes(), public, nil
}

func sendSSHPublicKey(instanceID, pubKey string) error {
	svc := ec2instanceconnect.New(session.New())
	input := &ec2instanceconnect.SendSSHPublicKeyInput{
		AvailabilityZone: aws.String("ap-northeast-1a"),
		InstanceId:       aws.String(instanceID),
		InstanceOSUser:   aws.String("ubuntu"),
		SSHPublicKey:     aws.String(pubKey),
	}
	result, err := svc.SendSSHPublicKey(input)
	if err != nil {
		return err
	}
	if *(result.Success) != true {
		return errors.New("sendSSHPublicKey failed")
	}
	return nil
}

func keyAuth(privKey []byte) ssh.AuthMethod {
	signer, _ := ssh.ParsePrivateKey(privKey)
	return ssh.PublicKeys(signer)
}
