package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"github.com/howeyc/gopass"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"
)

const DEFAULT_KEYS_LOCATION = "/.ssh/"
const RSA_KEY_SIZE = 2048
const FILE_PREFIX = "fubar"

type baseConfig struct {
	Gen_Cert_URLS string
	//UserAuth          string
}

type AppConfigFile struct {
	Base baseConfig
}

var (
	configFilename = flag.String("config", "config.yml", "The filename of the configuration")
)

func getUserHomeDir() (string, error) {
	// TODO: verify on Windows... see: http://stackoverflow.com/questions/7922270/obtain-users-home-directory
	usr, err := user.Current()
	if err != nil {
		log.Printf("cannot get current user info")
		return "", err
	}
	return usr.HomeDir, nil
}

// generateKeyPair uses internal golan functions to be portable
// mostly comes from: http://stackoverflow.com/questions/21151714/go-generate-an-ssh-public-key
func genKeyPair(BasePath string, prefix string) (string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, RSA_KEY_SIZE)
	if err != nil {
		return "", err
	}

	privateKeyPath := BasePath + prefix
	pubKeyPath := privateKeyPath + ".pub"

	// TODO: instead of deleteing here... create and then do atomic swap
	os.Remove(privateKeyPath)
	os.Remove(pubKeyPath)

	// generate and write private key as PEM
	privateKeyFile, err := os.Create(privateKeyPath)
	defer privateKeyFile.Close()
	if err != nil {
		log.Printf("Failed to save privkey")
		return "", err
	}
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return "", err
	}

	// generate and write public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", err
	}
	return pubKeyPath, ioutil.WriteFile(pubKeyPath, ssh.MarshalAuthorizedKey(pub), 0644)
}

func loadVerifyConfigFile(configFilename string) (AppConfigFile, error) {
	var config AppConfigFile
	if _, err := os.Stat(configFilename); os.IsNotExist(err) {
		err = errors.New("mising config file failure")
		return config, err
	}
	source, err := ioutil.ReadFile(configFilename)
	if err != nil {
		//panic(err)
		err = errors.New("cannot read config file")
		return config, err
	}
	err = yaml.Unmarshal(source, &config)
	if err != nil {
		err = errors.New("Cannot parse config file")
		return config, err
	}
	// TODO: actually have to verify the contents
	return config, nil
}

func main() {
	flag.Parse()

	config, err := loadVerifyConfigFile(*configFilename)
	if err != nil {
		panic(err)
	}

	usr, err := user.Current()
	if err != nil {
		log.Printf("cannot get current user info")
		log.Fatal(err)
	}
	userName := usr.Username

	fmt.Printf("Password for %s: ", userName)
	password, err := gopass.GetPasswd()
	if err != nil {
		log.Fatal(err)
		// Handle gopass.ErrInterrupted or getch() read error
	}
	// Do something with pass

	homeDir, err := getUserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	sshPath := homeDir + "/.ssh/"
	pubKeyFilename, err := genKeyPair(sshPath, FILE_PREFIX)
	if err != nil {
		log.Fatal(err)
	}
    
	success := false
	for _, baseUrl := range strings.Split(config.Base.Gen_Cert_URLS, ",") {
		targetUrl := baseUrl + userName
		log.Printf("attempting to target '%s'", targetUrl)
		// parts from  https://astaxie.gitbooks.io/build-web-application-with-golang/content/en/04.5.html
		bodyBuf := &bytes.Buffer{}
		bodyWriter := multipart.NewWriter(bodyBuf)

		//
		fileWriter, err := bodyWriter.CreateFormFile("pubkeyfile", pubKeyFilename)
		if err != nil {
			fmt.Println("error writing to buffer")
			log.Fatal(err)
		}

		// open file handle
		fh, err := os.Open(pubKeyFilename)
		if err != nil {
			fmt.Println("error opening file")
			//return err
			log.Fatal(err)
		}

		//iocopy
		_, err = io.Copy(fileWriter, fh)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("%v", bodyBuf)
		contentType := bodyWriter.FormDataContentType()
		bodyWriter.Close()

		client := &http.Client{Timeout: time.Duration(5) * time.Second}
		req, err := http.NewRequest("POST", targetUrl, bodyBuf)
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)

		// TODO: change basic auth for some form of tokens.
		req.SetBasicAuth(userName, string(password[:]))
		resp, err := client.Do(req) //client.Get(targetUrl)
		if err != nil {
			log.Printf("got error from req")
			log.Println(err)
			//TODO: differentialte between 400 and 500 errors
			//is OK to fail.. try next
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			log.Printf("got error from call %s", resp.Status)
			continue
		}
        body, err := ioutil.ReadAll(resp.Body)
        log.Printf("%s", body)

		// now save the file
		success = true
		break
	}
	if !success {
		log.Fatal("failed to get creds")
	}
	log.Printf("Success")

	// post to the signers

}
