package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type DemonioRequest struct {
	Ejecutable string `json:"ejecutable"`
	Puerto     string `json:"puerto"`
	Zip        string `json:"zip"`
	Plantilla  string `json:"plantilla"`
	Disco      string `json:"disco"`
}

type VM struct {
	Nombre string `json:"Nombre"`
	IP     string `json:"IP"`
	Puerto string `json:"Puerto"`
}

var discos []string
var vms []VM

func ejecutar(cmd string) string {
	command := exec.Command("bash", "-c", cmd)
	var out bytes.Buffer
	command.Stdout = &out
	command.Stderr = &out
	command.Run()
	return strings.TrimSpace(out.String())
}

func crearDemonio(w http.ResponseWriter, r *http.Request) {
	var req DemonioRequest
	json.NewDecoder(r.Body).Decode(&req)
	usuario := "diego"

	// Copiar archivos a la MV
	ejecutar(fmt.Sprintf("scp %s %s@%s:/home/%s/", req.Ejecutable, usuario, req.Plantilla, usuario))
	ejecutar(fmt.Sprintf("scp %s %s@%s:/home/%s/", req.Zip, usuario, req.Plantilla, usuario))

	// Crear servicio systemd
	service := fmt.Sprintf(`[Unit]
Description=Servicio Web Parcial

[Service]
ExecStart=/home/%s/%s
Restart=always

[Install]
WantedBy=multi-user.target
`, usuario, req.Ejecutable)

	ejecutar(fmt.Sprintf(`ssh %s@%s 'echo "%s" | sudo tee /etc/systemd/system/app.service'`, usuario, req.Plantilla, service))
	ejecutar(fmt.Sprintf("ssh %s@%s 'sudo systemctl daemon-reload'", usuario, req.Plantilla))
	ejecutar(fmt.Sprintf("ssh %s@%s 'sudo systemctl enable app'", usuario, req.Plantilla))
	ejecutar(fmt.Sprintf("ssh %s@%s 'sudo systemctl start app'", usuario, req.Plantilla))

	// Convertir disco a multiconexión
	ejecutar(fmt.Sprintf("VBoxManage modifyhd %s.vdi --type multiattach", req.Disco))
	discos = append(discos, req.Disco)

	w.Write([]byte("Demonio creado"))
}

func listaDiscos(w http.ResponseWriter, r *http.Request) {
	if discos == nil {
		discos = []string{}
	}
	json.NewEncoder(w).Encode(discos)
}

func crearVM(w http.ResponseWriter, r *http.Request) {
	nombre := r.URL.Query().Get("nombre")
	disco := r.URL.Query().Get("disco")
	puerto := r.URL.Query().Get("puerto")

	// Crear VM
	ejecutar(fmt.Sprintf("VBoxManage createvm --name %s --register", nombre))
	ejecutar(fmt.Sprintf("VBoxManage storagectl %s --name SATA --add sata --controller IntelAhci", nombre))
	ejecutar(fmt.Sprintf("VBoxManage storageattach %s --storagectl SATA --port 0 --device 0 --type hdd --medium %s.vdi", nombre, disco))
	ejecutar(fmt.Sprintf("VBoxManage startvm %s --type headless", nombre))

	// Obtener IP automáticamente (espera hasta obtener IP)
	ip := ""
	for i := 0; i < 30; i++ { // Espera hasta 30 seg
		ip = obtenerIP(nombre)
		if ip != "" && ip != "0.0.0.0" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	vm := VM{
		Nombre: nombre,
		IP:     ip,
		Puerto: puerto,
	}
	vms = append(vms, vm)
	json.NewEncoder(w).Encode(vm)
}

func obtenerIP(vm string) string {
	out := ejecutar(fmt.Sprintf("VBoxManage guestproperty get %s /VirtualBox/GuestInfo/Net/0/V4/IP", vm))
	if strings.Contains(out, "Value:") {
		ip := strings.Split(out, "Value:")[1]
		return strings.TrimSpace(ip)
	}
	return ""
}

func listaVM(w http.ResponseWriter, r *http.Request) {
	if vms == nil {
		vms = []VM{}
	}
	json.NewEncoder(w).Encode(vms)
}

func apagarVM(w http.ResponseWriter, r *http.Request) {
	nombre := r.URL.Query().Get("nombre")
	ejecutar(fmt.Sprintf("VBoxManage controlvm %s poweroff", nombre))
	w.Write([]byte("VM apagada"))
}

func main() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "templates/index.html")
	})

	http.HandleFunc("/crear-demonio", crearDemonio)
	http.HandleFunc("/discos", listaDiscos)
	http.HandleFunc("/crear-vm", crearVM)
	http.HandleFunc("/vms", listaVM)
	http.HandleFunc("/apagar-vm", apagarVM)

	fmt.Println("Servidor en http://localhost:8081")
	http.ListenAndServe(":8081", nil)
}