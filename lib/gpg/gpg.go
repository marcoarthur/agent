// All kind of operations related to Subutai PKI are gathered in gpg package
package gpg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/clearsign"

	"github.com/subutai-io/agent/agent/utils"
	"github.com/subutai-io/agent/config"
	"github.com/subutai-io/agent/lib/container"
	"github.com/subutai-io/agent/log"
	"time"
	"path"
)

var (
	GPG = "gpg1"
)

//ImportPk imports Public Key "gpg2 --import pubkey.key".
func ImportPk(k []byte) string {
	tmpfile, err := ioutil.TempFile("", "subutai-epub")
	if !log.Check(log.WarnLevel, "Creating Public key file", err) {

		_, err = tmpfile.Write(k)
		log.Check(log.WarnLevel, "Writing Management server Public key to "+tmpfile.Name(), err)
		log.Check(log.WarnLevel, "Closing "+tmpfile.Name(), tmpfile.Close())

		out, err := exec.Command(GPG, "--import", tmpfile.Name()).CombinedOutput()
		log.Check(log.WarnLevel, "Importing MH Public key from "+tmpfile.Name(), err)
		log.Check(log.WarnLevel, "Removing temp file", os.Remove(tmpfile.Name()))
		return string(out)
	}
	return err.Error()
}

// GetContainerPk returns GPG Public Key for container.
func GetContainerPk(name string) string {
	lxcPath := path.Join(config.Agent.LxcPrefix, name, "public.pub")
	stdout, err := exec.Command("/bin/bash", "-c", GPG+" --no-default-keyring --keyring "+lxcPath+" --export -a "+name+"@subutai.io").Output()
	log.Check(log.WarnLevel, "Getting Container public key", err)
	return string(stdout)
}

// GetPk returns GPG Public Key from the Resource Host.
func GetPk(name string) string {
	stdout, err := exec.Command(GPG, "--export", "-a", name).Output()
	log.Check(log.WarnLevel, "Getting public key", err)
	if len(stdout) == 0 {
		log.Warn("GPG key for RH not found. Creating new.")
		GenerateKey(name)
	}
	return string(stdout)
}

// DecryptWrapper decrypts GPG message.
func DecryptWrapper(args ...string) string {
	gpg := GPG + " --passphrase " + config.Agent.GpgPassword + " --no-tty"
	if len(args) == 3 {
		gpg = gpg + " --no-default-keyring --keyring " + args[2] + " --secret-keyring " + args[1]
	}
	command := exec.Command("/bin/bash", "-c", gpg)
	stdin, err := command.StdinPipe()
	if err == nil {
		_, err = stdin.Write([]byte(args[0]))
		log.Check(log.DebugLevel, "Writing to stdin of gpg", err)
		log.Check(log.DebugLevel, "Closing stdin of gpg", stdin.Close())
	}

	output, err := command.Output()
	log.Check(log.WarnLevel, "Executing command "+gpg, err)

	return string(output)
}

// EncryptWrapper encrypts GPG message.
func EncryptWrapper(user, recipient string, message []byte, args ...string) ([]byte, error) {
	gpg := GPG + " --batch --passphrase " + config.Agent.GpgPassword + " --trust-model always --armor -u " + user + " -r " + recipient + " --sign --encrypt --no-tty"
	if len(args) >= 2 {
		gpg = gpg + " --no-default-keyring --keyring " + args[0] + " --secret-keyring " + args[1]
	}
	command := exec.Command("/bin/bash", "-c", gpg)
	stdin, err := command.StdinPipe()
	if err == nil {
		_, err = stdin.Write(message)
		log.Check(log.DebugLevel, "Writing to stdin of gpg", err)
		log.Check(log.DebugLevel, "Closing stdin of gpg", stdin.Close())
	}
	return command.Output()
}

// GenerateKey generates GPG-key for Subutai Agent.
// This key used for encrypting messages for Subutai Agent.
func GenerateKey(name string) {
	thePath := path.Join(config.Agent.LxcPrefix , name)
	email := name + "@subutai.io"
	pass := config.Agent.GpgPassword
	if !container.LxcInstanceExists(name) {
		err := os.MkdirAll("/root/.gnupg/", 0700)
		log.Check(log.DebugLevel, "Creating /root/.gnupg/", err)
		thePath = "/root/.gnupg"
		email = name
		pass = config.Agent.GpgPassword
	}
	conf, err := os.Create(thePath + "/defaults")
	if log.Check(log.FatalLevel, "Writing default key ident", err) {
		return
	}
	_, err = conf.WriteString("%echo Generating default keys\n" +
		"Key-Type: RSA\n" +
		"Key-Length: 2048\n" +
		"Name-Real: " + name + "\n" +
		"Name-Comment: " + name + " GPG key\n" +
		"Name-Email: " + email + "\n" +
		"Expire-Date: 0\n" +
		"Passphrase: " + pass + "\n" +
		"%pubring " + thePath + "/public.pub\n" +
		"%secring " + thePath + "/secret.sec\n" +
		"%commit\n" +
		"%echo Done\n")
	log.Check(log.DebugLevel, "Writing defaults for gpg", err)
	log.Check(log.DebugLevel, "Closing defaults for gpg", conf.Close())

	if _, err := os.Stat(thePath + "/secret.sec"); os.IsNotExist(err) {
		log.Check(log.DebugLevel, "Generating key", exec.Command(GPG, "--batch", "--gen-key", thePath+"/defaults").Run())
	}
	if !container.LxcInstanceExists(name) {
		out, err := exec.Command(GPG, "--allow-secret-key-import", "--import", "/root/.gnupg/secret.sec").CombinedOutput()
		if log.Check(log.DebugLevel, "Importing secret key "+string(out), err) {
			list, _ := filepath.Glob(filepath.Join(config.Agent.DataPrefix+".gnupg", "*.lock"))
			for _, f := range list {
				os.Remove(f)
			}
		}
		out, err = exec.Command(GPG, "--import", "/root/.gnupg/public.pub").CombinedOutput()
		if log.Check(log.DebugLevel, "Importing public key "+string(out), err) {
			list, _ := filepath.Glob(filepath.Join(config.Agent.DataPrefix+".gnupg", "*.lock"))
			for _, f := range list {
				os.Remove(f)
			}
		}
	}
}

// GetFingerprint returns fingerprint of the Subutai container.
func GetFingerprint(email string) string {
	var out []byte
	var err error
	if email == config.Agent.GpgUser {
		out, err = exec.Command(GPG, "--fingerprint", email).Output()
		log.Check(log.DebugLevel, "Getting fingerprint by "+email, err)
	} else {
		out, err = exec.Command(GPG, "--fingerprint", "--keyring", path.Join(config.Agent.LxcPrefix,email,"public.pub"), email).Output()

		log.Check(log.DebugLevel, "Getting fingerprint by "+email, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "fingerprint") {
			fp := strings.Split(scanner.Text(), "=")
			if len(fp) > 1 {
				return strings.Replace(fp[1], " ", "", -1)
			}
		}
	}
	return ""
}

func getMngKey(c string) {
	client := utils.GetClient(config.Management.Allowinsecure, 5)
	resp, err := client.Get("https://" + path.Join(config.Management.Host) + ":" + config.Management.Port + config.Management.RestPublicKey)
	log.Check(log.FatalLevel, "Getting Management public key", err)

	defer utils.Close(resp)

	if body, err := ioutil.ReadAll(resp.Body); err == nil {
		err = ioutil.WriteFile(path.Join(config.Agent.LxcPrefix,c,"mgn.key"), body, 0644)
		log.Check(log.FatalLevel, "Writing Management public key", err)
	}
}

func parseKeyID(s string) string {
	var id string

	line := strings.Split(s, "\n")
	if len(line) > 2 {
		cell := strings.Split(line[1], " ")
		if len(cell) > 3 {
			key := strings.Split(cell[3], "/")
			if len(key) > 1 {
				id = key[1]
			}
		}
	}
	if len(id) == 0 {
		log.Fatal("Key id parsing error")
	}
	return id
}

func writeData(c, t, n, m string) {
	log.Check(log.DebugLevel, "Removing "+path.Join(config.Agent.LxcPrefix,c,"stdin.txt.asc"), os.Remove(path.Join(config.Agent.LxcPrefix,c,"stdin.txt.asc")))
	log.Check(log.DebugLevel, "Removing "+path.Join(config.Agent.LxcPrefix,c,"stdin.txt"), os.Remove(path.Join(config.Agent.LxcPrefix,c,"stdin.txt")))

	token := []byte(t + "\n" + GetFingerprint(c) + "\n" + n + m)
	err := ioutil.WriteFile(path.Join(config.Agent.LxcPrefix,c,"stdin.txt"), token, 0644)
	log.Check(log.FatalLevel, "Writing Management public key", err)
}

func sendData(c string) {
	asc, err := os.Open(path.Join(config.Agent.LxcPrefix,c,"stdin.txt.asc"))
	log.Check(log.FatalLevel, "Reading encrypted stdin.txt.asc", err)
	defer asc.Close()

	client := utils.TLSConfig()
	client.Timeout = time.Second * 15
	resp, err := client.Post("https://"+path.Join(config.Management.Host)+":8444/rest/v1/registration/verify/container-token", "text/plain", asc)
	log.Check(log.DebugLevel, "Removing "+path.Join(config.Agent.LxcPrefix,c,"stdin.txt.asc"), os.Remove(path.Join(config.Agent.LxcPrefix,c,"stdin.txt.asc")))
	log.Check(log.DebugLevel, "Removing "+path.Join(config.Agent.LxcPrefix,c,"stdin.txt"), os.Remove(path.Join(config.Agent.LxcPrefix,c,"stdin.txt")))
	log.Check(log.FatalLevel, "Sending registration request to management", err)
	defer utils.Close(resp)
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		log.Error("Failed to exchange GPG Public Keys. StatusCode: " + resp.Status)
	}

}

// ExchageAndEncrypt installing the Management server GPG public key to the container keyring.
// Sending container's GPG public key to the Management server. It require encrypting and singing message
// received from the Management server.
func ExchageAndEncrypt(c, t string) {
	var impout, expout, imperr, experr bytes.Buffer

	getMngKey(c)

	impkey := exec.Command(GPG, "-v", "--no-default-keyring", "--keyring", path.Join(config.Agent.LxcPrefix,c,"public.pub"), "--import", path.Join(config.Agent.LxcPrefix,c,"mgn.key"))
	impkey.Stdout = &impout
	impkey.Stderr = &imperr
	err := impkey.Run()
	log.Check(log.FatalLevel, "Importing Management public key to keyring", err)

	id := parseKeyID(imperr.String())
	expkey := exec.Command(GPG, "--no-default-keyring", "--keyring",path.Join(config.Agent.LxcPrefix,c,"public.pub") , "--export", "--armor", c+"@subutai.io")
	expkey.Stdout = &expout
	expkey.Stderr = &experr
	err = expkey.Run()
	log.Check(log.FatalLevel, "Exporting armomred key", err)

	writeData(c, t, expout.String(), experr.String())

	err = exec.Command(GPG, "--no-default-keyring", "--keyring", path.Join(config.Agent.LxcPrefix,c,"public.pub"), "--trust-model", "always", "--armor", "-r", id, "--encrypt", path.Join(config.Agent.LxcPrefix,c,"stdin.txt")).Run()
	log.Check(log.FatalLevel, "Encrypting stdin.txt", err)

	sendData(c)
}

// ValidatePem checks if OpenSSL x509 certificate valid.
func ValidatePem(cert string) bool {
	out, err := exec.Command("openssl", "x509", "-in", cert, "-text", "-noout").Output()
	log.Check(log.DebugLevel, "Validating OpenSSL x509 certificate", err)
	return strings.Contains(string(out), "Public Key") && strings.Contains(string(out), "X509")
}

// ParsePem return parsed OpenSSL x509 certificate.
func ParsePem(cert string) (crt, key []byte) {
	var err error
	if key, err = exec.Command("openssl", "pkey", "-in", cert).Output(); err == nil {
		f, err := ioutil.ReadFile(cert)
		if !log.Check(log.DebugLevel, "Cannot read file "+cert, err) {
			crt = bytes.Replace(f, key, []byte(""), -1)
		}
	}
	return crt, key
}
//todo move to CDN related package
// KurjunUserPK gets user's public GPG-key from Kurjun.
func KurjunUserPK(owner string) []string {
	utils.CheckCDN()

	var keys []string
	kurjun := utils.GetClient(config.CDN.Allowinsecure, 15)
	response, err := kurjun.Get(config.CDN.Kurjun + "/auth/keys?user=" + owner)
	log.Check(log.FatalLevel, "Getting owner public key", err)
	defer utils.Close(response)

	key, err := ioutil.ReadAll(response.Body)
	log.Check(log.FatalLevel, "Reading key body", err)
	if json.Unmarshal(key, &keys) == nil {
		return keys
	}
	return nil
}

// VerifySignature check if signature retrieved from Kurjun is valid.
func VerifySignature(key, signature string) string {
	entity, err := openpgp.ReadArmoredKeyRing(bytes.NewBufferString(key))
	log.Check(log.WarnLevel, "Reading user public key", err)

	if block, _ := clearsign.Decode([]byte(signature)); block != nil {
		_, err = openpgp.CheckDetachedSignature(entity, bytes.NewBuffer(block.Bytes), block.ArmoredSignature.Body)
		if !log.Check(log.DebugLevel, "Checking signature", err) {
			return string(block.Bytes)
		}
	}
	return ""
}

func ExtractKeyID(k []byte) string {
	command := exec.Command(GPG)
	stdin, err := command.StdinPipe()
	if err != nil {
		return ""
	}

	_, err = stdin.Write(k)
	log.Check(log.DebugLevel, "Writing to stdin pipe", err)
	log.Check(log.DebugLevel, "Closing stdin pipe", stdin.Close())
	out, err := command.Output()
	log.Check(log.WarnLevel, "Extracting ID from Key", err)

	if line := strings.Fields(string(out)); len(line) > 1 {
		if key := strings.Split(line[1], "/"); len(key) > 1 {
			return key[1]
		}
	}
	return ""
}
