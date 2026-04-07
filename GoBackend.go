package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ─────────────────────────────────────────────
// MODELOS
// ─────────────────────────────────────────────

type PrepararRequest struct {
	Ejecutable   string `json:"ejecutable"`   // ruta local del binario
	Puerto       string `json:"puerto"`       // ej. "8081"
	ZipAdicional string `json:"zip"`          // ruta local del .zip
	VMPlantilla  string `json:"vm_plantilla"` // nombre de la VM base en VirtualBox
	NombreDisco  string `json:"nombre_disco"` // nombre del nuevo disco multiconexión
}

type CrearVMRequest struct {
	NombreVM    string `json:"nombre_vm"`   // nombre para la nueva VM
	NombreDisco string `json:"nombre_disco"` // disco multiconexión origen
}

type ServiceRequest struct {
	NombreVM string `json:"nombre_vm"` // VM objetivo
	Accion   string `json:"accion"`    // start | stop | restart | enable | disable | status | logs
}

type Respuesta struct {
	OK      bool   `json:"ok"`
	Mensaje string `json:"mensaje"`
	Salida  string `json:"salida,omitempty"`
}

type DiscoInfo struct {
	Nombre string `json:"nombre"`
	Ruta   string `json:"ruta"`
}

type VMInfo struct {
	Nombre string `json:"nombre"`
	IP     string `json:"ip"`
	Puerto string `json:"puerto"`
	Estado string `json:"estado"`
}

// ─────────────────────────────────────────────
// CONFIGURACIÓN SSH
// ─────────────────────────────────────────────

const (
	SSHUser    = "admin"          // usuario en la VM
	SSHKeyPath = "/root/.ssh/id_rsa" // llave privada del servidor de gestión
	AppDestDir = "/opt/servicio"  // directorio destino en la VM
	ServiceDir = "/etc/systemd/system"
)

// ─────────────────────────────────────────────
// UTILIDADES SSH
// ─────────────────────────────────────────────

func sshClient(ip string) (*ssh.Client, error) {
	key, err := os.ReadFile(SSHKeyPath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la llave SSH: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("llave SSH inválida: %w", err)
	}
	config := &ssh.ClientConfig{
		User:            SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // en producción usar known_hosts
		Timeout:         15 * time.Second,
	}
	return ssh.Dial("tcp", ip+":22", config)
}

// ejecutarSSH corre un comando remoto y devuelve stdout+stderr combinados
func ejecutarSSH(ip, cmd string) (string, error) {
	client, err := sshClient(ip)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput(cmd)
	return string(out), err
}

// copiarArchivoSSH sube un archivo local a la VM via SCP emulado con SFTP
func copiarArchivoSSH(ip, localPath, remotePath string) error {
	client, err := sshClient(ip)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C0755 %d %s\n", stat.Size(), filepath.Base(remotePath))
		io.Copy(w, f)
		fmt.Fprint(w, "\x00")
	}()

	dir := filepath.Dir(remotePath)
	return session.Run(fmt.Sprintf("mkdir -p %s && scp -t %s", dir, dir))
}

// ─────────────────────────────────────────────
// UTILIDADES VBOXMANAGE
// ─────────────────────────────────────────────

func vbox(args ...string) (string, error) {
	out, err := exec.Command("VBoxManage", args...).CombinedOutput()
	return string(out), err
}

// obtenerIPdeVM espera hasta 60 s a que la VM reporte una IP via GuestProperties
func obtenerIPdeVM(nombre string) (string, error) {
	for i := 0; i < 12; i++ {
		out, err := vbox("guestproperty", "get", nombre, "/VirtualBox/GuestInfo/Net/0/V4/IP")
		if err == nil && strings.Contains(out, "Value:") {
			partes := strings.Split(out, "Value:")
			return strings.TrimSpace(partes[1]), nil
		}
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("la VM no reportó IP en 60 segundos")
}

// ─────────────────────────────────────────────
// LÓGICA PRINCIPAL: PREPARAR DEMONIO
// ─────────────────────────────────────────────

// prepararDemonio:
// 1. Arranca la VM plantilla
// 2. Copia el ejecutable y el .zip a la VM
// 3. Descomprime el .zip
// 4. Crea y activa el servicio systemd
// 5. Apaga la VM
// 6. Convierte su disco a multiconexión y lo desacopla
func prepararDemonio(req PrepararRequest) error {
	log.Printf("[PREP] Iniciando VM plantilla: %s", req.VMPlantilla)

	// 1. Iniciar VM plantilla
	if _, err := vbox("startvm", req.VMPlantilla, "--type", "headless"); err != nil {
		return fmt.Errorf("no se pudo iniciar la VM plantilla: %w", err)
	}
	time.Sleep(20 * time.Second) // esperar boot

	ip, err := obtenerIPdeVM(req.VMPlantilla)
	if err != nil {
		return err
	}
	log.Printf("[PREP] IP plantilla: %s", ip)

	// 2. Crear directorio destino
	if _, err := ejecutarSSH(ip, fmt.Sprintf("mkdir -p %s", AppDestDir)); err != nil {
		return fmt.Errorf("error creando directorio en VM: %w", err)
	}

	// 3. Copiar ejecutable
	destEjec := filepath.Join(AppDestDir, filepath.Base(req.Ejecutable))
	if err := copiarArchivoSSH(ip, req.Ejecutable, destEjec); err != nil {
		return fmt.Errorf("error copiando ejecutable: %w", err)
	}
	ejecutarSSH(ip, fmt.Sprintf("chmod +x %s", destEjec))

	// 4. Copiar y descomprimir .zip si existe
	if req.ZipAdicional != "" {
		destZip := filepath.Join(AppDestDir, "recursos.zip")
		if err := copiarArchivoSSH(ip, req.ZipAdicional, destZip); err != nil {
			return fmt.Errorf("error copiando .zip: %w", err)
		}
		ejecutarSSH(ip, fmt.Sprintf("cd %s && unzip -o recursos.zip", AppDestDir))
	}

	// 5. Crear archivo .service de systemd
	nombreServicio := req.NombreDisco
	serviceContent := fmt.Sprintf(`[Unit]
Description=Servicio gestionado: %s
After=network.target

[Service]
Type=simple
ExecStart=%s
WorkingDirectory=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, nombreServicio, destEjec, AppDestDir)

	// Escribir el .service via heredoc
	cmdService := fmt.Sprintf(
		"cat > %s/%s.service << 'EOF'\n%sEOF",
		ServiceDir, nombreServicio, serviceContent,
	)
	if out, err := ejecutarSSH(ip, cmdService); err != nil {
		return fmt.Errorf("error creando .service: %s %w", out, err)
	}

	// Recargar systemd y habilitar servicio
	cmds := []string{
		"systemctl daemon-reload",
		fmt.Sprintf("systemctl enable %s.service", nombreServicio),
		fmt.Sprintf("systemctl start %s.service", nombreServicio),
	}
	for _, c := range cmds {
		if out, err := ejecutarSSH(ip, c); err != nil {
			log.Printf("[WARN] %s: %s", c, out)
		}
	}

	// 6. Apagar VM limpiamente
	ejecutarSSH(ip, "shutdown -h now")
	time.Sleep(10 * time.Second)

	// 7. Convertir disco a multiconexión
	// Primero obtener la ruta del disco asociado a la VM
	infoVM, err := vbox("showvminfo", req.VMPlantilla, "--machinereadable")
	if err != nil {
		return fmt.Errorf("no se pudo obtener info de VM: %w", err)
	}
	discoOrigen := extraerRutaDisco(infoVM)
	if discoOrigen == "" {
		return fmt.Errorf("no se encontró disco en la VM plantilla")
	}

	// Desacoplar disco de la VM
	vbox("storageattach", req.VMPlantilla,
		"--storagectl", "SATA",
		"--port", "0",
		"--device", "0",
		"--medium", "none")

	// Clonar como disco multiconexión
	discoDest := filepath.Join(filepath.Dir(discoOrigen), req.NombreDisco+".vdi")
	if _, err := vbox("clonemedium", "disk", discoOrigen, discoDest,
		"--variant", "Standard"); err != nil {
		return fmt.Errorf("error clonando disco: %w", err)
	}

	// Cambiar tipo a multiattach
	if _, err := vbox("modifymedium", "disk", discoDest,
		"--type", "multiattach"); err != nil {
		return fmt.Errorf("error convirtiendo a multiconexión: %w", err)
	}

	log.Printf("[PREP] Disco multiconexión listo: %s", discoDest)
	return nil
}

// extraerRutaDisco parsea la salida de showvminfo para obtener la ruta del primer disco
func extraerRutaDisco(info string) string {
	for _, linea := range strings.Split(info, "\n") {
		if strings.HasPrefix(linea, `"SATA-0-0"=`) {
			partes := strings.SplitN(linea, "=", 2)
			if len(partes) == 2 {
				return strings.Trim(partes[1], `"`)
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// LÓGICA: CREAR VM DESDE DISCO MULTICONEXIÓN
// ─────────────────────────────────────────────

func crearVM(req CrearVMRequest, puerto string) (VMInfo, error) {
	// Buscar ruta del disco multiconexión registrado
	out, err := vbox("list", "hdds")
	if err != nil {
		return VMInfo{}, err
	}
	rutaDisco := buscarDisco(out, req.NombreDisco)
	if rutaDisco == "" {
		return VMInfo{}, fmt.Errorf("disco '%s' no encontrado en VirtualBox", req.NombreDisco)
	}

	// Crear VM
	vbox("createvm", "--name", req.NombreVM, "--ostype", "Debian_64", "--register")

	// Configurar hardware
	vbox("modifyvm", req.NombreVM,
		"--memory", "512",
		"--cpus", "1",
		"--nic1", "hostonly",
		"--hostonlyadapter1", "vboxnet0",
	)

	// Agregar controlador SATA
	vbox("storagectl", req.NombreVM,
		"--name", "SATA",
		"--add", "sata",
		"--controller", "IntelAhci",
	)

	// Adjuntar disco multiconexión
	_, err = vbox("storageattach", req.NombreVM,
		"--storagectl", "SATA",
		"--port", "0",
		"--device", "0",
		"--type", "hdd",
		"--medium", rutaDisco,
	)
	if err != nil {
		return VMInfo{}, fmt.Errorf("error adjuntando disco: %w", err)
	}

	// Iniciar VM
	if _, err := vbox("startvm", req.NombreVM, "--type", "headless"); err != nil {
		return VMInfo{}, fmt.Errorf("error iniciando VM: %w", err)
	}

	// Esperar IP
	ip, err := obtenerIPdeVM(req.NombreVM)
	if err != nil {
		return VMInfo{}, err
	}

	return VMInfo{
		Nombre: req.NombreVM,
		IP:     ip,
		Puerto: puerto,
		Estado: "running",
	}, nil
}

func buscarDisco(lista, nombre string) string {
	bloques := strings.Split(lista, "\n\n")
	for _, bloque := range bloques {
		if strings.Contains(bloque, nombre) {
			for _, linea := range strings.Split(bloque, "\n") {
				if strings.HasPrefix(linea, "Location:") {
					return strings.TrimSpace(strings.TrimPrefix(linea, "Location:"))
				}
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// LÓGICA: GESTIÓN SYSTEMD
// ─────────────────────────────────────────────

func gestionarServicio(req ServiceRequest, ipVM string, nombreServicio string) (string, error) {
	var cmd string
	switch req.Accion {
	case "start":
		cmd = fmt.Sprintf("systemctl start %s.service", nombreServicio)
	case "stop":
		cmd = fmt.Sprintf("systemctl stop %s.service", nombreServicio)
	case "restart":
		cmd = fmt.Sprintf("systemctl restart %s.service", nombreServicio)
	case "enable":
		cmd = fmt.Sprintf("systemctl enable %s.service", nombreServicio)
	case "disable":
		cmd = fmt.Sprintf("systemctl disable %s.service", nombreServicio)
	case "status":
		cmd = fmt.Sprintf("systemctl status %s.service --no-pager", nombreServicio)
	case "logs":
		cmd = fmt.Sprintf("journalctl -u %s.service -n 50 --no-pager", nombreServicio)
	default:
		return "", fmt.Errorf("acción desconocida: %s", req.Accion)
	}
	return ejecutarSSH(ipVM, cmd)
}

// ─────────────────────────────────────────────
// HANDLERS HTTP
// ─────────────────────────────────────────────

func jsonResp(w http.ResponseWriter, status int, r Respuesta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(r)
}

// POST /api/preparar
// Body: PrepararRequest (multipart/form-data o JSON)
func handlerPreparar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, Respuesta{Mensaje: "método no permitido"})
		return
	}

	// Parsear multipart para recibir archivos + campos
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		jsonResp(w, 400, Respuesta{Mensaje: "error parseando formulario: " + err.Error()})
		return
	}

	req := PrepararRequest{
		Puerto:      r.FormValue("puerto"),
		VMPlantilla: r.FormValue("vm_plantilla"),
		NombreDisco: r.FormValue("nombre_disco"),
	}

	// Guardar ejecutable subido
	req.Ejecutable, _ = guardarArchivo(r, "ejecutable", "/tmp")
	// Guardar zip subido
	req.ZipAdicional, _ = guardarArchivo(r, "zip", "/tmp")

	if err := prepararDemonio(req); err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: err.Error()})
		return
	}
	jsonResp(w, 200, Respuesta{OK: true, Mensaje: "Disco multiconexión creado correctamente"})
}

// guardarArchivo guarda un archivo del form en disco y devuelve su ruta
func guardarArchivo(r *http.Request, campo, dir string) (string, error) {
	f, header, err := r.FormFile(campo)
	if err != nil {
		return "", err
	}
	defer f.Close()

	ruta := filepath.Join(dir, header.Filename)
	dst, err := os.Create(ruta)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	io.Copy(dst, f)
	return ruta, nil
}

// POST /api/crear-vm
func handlerCrearVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, Respuesta{Mensaje: "método no permitido"})
		return
	}
	var req struct {
		CrearVMRequest
		Puerto string `json:"puerto"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResp(w, 400, Respuesta{Mensaje: "JSON inválido: " + err.Error()})
		return
	}

	info, err := crearVM(req.CrearVMRequest, req.Puerto)
	if err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// GET /api/discos
func handlerListarDiscos(w http.ResponseWriter, r *http.Request) {
	out, err := vbox("list", "hdds")
	if err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: err.Error()})
		return
	}

	var discos []DiscoInfo
	bloques := strings.Split(out, "\n\n")
	for _, bloque := range bloques {
		if !strings.Contains(bloque, "multiattach") {
			continue
		}
		d := DiscoInfo{}
		for _, linea := range strings.Split(bloque, "\n") {
			if strings.HasPrefix(linea, "Location:") {
				d.Ruta = strings.TrimSpace(strings.TrimPrefix(linea, "Location:"))
				d.Nombre = strings.TrimSuffix(filepath.Base(d.Ruta), ".vdi")
			}
		}
		if d.Nombre != "" {
			discos = append(discos, d)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(discos)
}

// GET /api/vms
func handlerListarVMs(w http.ResponseWriter, r *http.Request) {
	out, err := vbox("list", "runningvms")
	if err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: err.Error()})
		return
	}

	var vms []VMInfo
	for _, linea := range strings.Split(out, "\n") {
		if linea == "" {
			continue
		}
		// formato: "NombreVM" {uuid}
		partes := strings.Fields(linea)
		if len(partes) < 1 {
			continue
		}
		nombre := strings.Trim(partes[0], `"`)
		ip, _ := obtenerIPdeVM(nombre)
		vms = append(vms, VMInfo{
			Nombre: nombre,
			IP:     ip,
			Estado: "running",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vms)
}

// POST /api/apagar-vm
func handlerApagarVM(w http.ResponseWriter, r *http.Request) {
	var req struct{ Nombre string `json:"nombre"` }
	json.NewDecoder(r.Body).Decode(&req)
	out, err := vbox("controlvm", req.Nombre, "acpipowerbutton")
	if err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: string(out)})
		return
	}
	jsonResp(w, 200, Respuesta{OK: true, Mensaje: "VM apagándose..."})
}

// POST /api/servicio
// Body: { "nombre_vm": "Srv-img3x1a", "accion": "status", "ip": "10.0.48.19", "servicio": "Srv-img3x1" }
func handlerServicio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, Respuesta{Mensaje: "método no permitido"})
		return
	}
	var body struct {
		ServiceRequest
		IP       string `json:"ip"`
		Servicio string `json:"servicio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResp(w, 400, Respuesta{Mensaje: "JSON inválido"})
		return
	}

	salida, err := gestionarServicio(body.ServiceRequest, body.IP, body.Servicio)
	if err != nil {
		jsonResp(w, 500, Respuesta{Mensaje: err.Error(), Salida: salida})
		return
	}
	jsonResp(w, 200, Respuesta{OK: true, Salida: salida})
}

// ─────────────────────────────────────────────
// SERVIDOR DE ARCHIVOS ESTÁTICOS
// ─────────────────────────────────────────────

// descomprimirZip extrae un .zip en un directorio destino (utilidad auxiliar)
func descomprimirZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		ruta := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(ruta, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(ruta), 0755)
		rc, _ := f.Open()
		dst, _ := os.Create(ruta)
		io.Copy(dst, rc)
		rc.Close()
		dst.Close()
	}
	return nil
}

// ─────────────────────────────────────────────
// MAIN
// ─────────────────────────────────────────────

func main() {
	mux := http.NewServeMux()

	// API REST
	mux.HandleFunc("/api/preparar", handlerPreparar)
	mux.HandleFunc("/api/crear-vm", handlerCrearVM)
	mux.HandleFunc("/api/discos", handlerListarDiscos)
	mux.HandleFunc("/api/vms", handlerListarVMs)
	mux.HandleFunc("/api/apagar-vm", handlerApagarVM)
	mux.HandleFunc("/api/servicio", handlerServicio)

	// Archivos estáticos (tu HTML va en ./static/index.html)
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	addr := ":8080"
	log.Printf("Servidor de gestión corriendo en http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}