package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ─────────────────────────────────────────────
// Configuración
// ─────────────────────────────────────────────

const (
	uploadDir      = "/tmp/daemon-manager"
	vboxManage     = "VBoxManage"
	systemctlBin   = "systemctl"
	serviceDestDir = "/etc/systemd/system"
)

// ─────────────────────────────────────────────
// Estructuras de respuesta
// ─────────────────────────────────────────────

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type Disk struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type VirtualMachine struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Port string `json:"port"`
}

type ServiceStatus struct {
	Service string `json:"service"`
	Output  string `json:"output"`
}

// ─────────────────────────────────────────────
// Utilidades
// ─────────────────────────────────────────────

func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func saveUploadedFile(r *http.Request, field string, destDir string) (string, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return "", fmt.Errorf("campo '%s' no encontrado: %w", field, err)
	}
	defer file.Close()

	dest := filepath.Join(destDir, header.Filename)
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	io.Copy(out, file)
	return dest, nil
}

// ─────────────────────────────────────────────
// Handler: Crear demonio
// POST /api/create-daemon
// Form-data: executable (file), port (string),
//            zipfile (file), vm_template (string),
//            disk_name (string)
// ─────────────────────────────────────────────

func createDaemonHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}
	r.ParseMultipartForm(64 << 20) // 64 MB

	port := r.FormValue("port")
	vmTemplate := r.FormValue("vm_template")
	diskName := r.FormValue("disk_name")

	if port == "" || vmTemplate == "" || diskName == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "port, vm_template y disk_name son requeridos"})
		return
	}

	// 1. Guardar ejecutable
	execPath, err := saveUploadedFile(r, "executable", uploadDir)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, Response{false, "error al guardar ejecutable: " + err.Error()})
		return
	}
	os.Chmod(execPath, 0755)

	// 2. Guardar zip de archivos adicionales
	zipPath, err := saveUploadedFile(r, "zipfile", uploadDir)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, Response{false, "error al guardar zip: " + err.Error()})
		return
	}

	// 3. Conectar a la VM plantilla por SSH y transferir archivos
	appDir := "/opt/daemon-app"
	ip := getVMIP(vmTemplate)
	if ip == "desconocida" || ip == "" {
		jsonResponse(w, http.StatusInternalServerError, Response{false, "no se pudo obtener la IP de la VM"})
		return
	}

	sshTarget := "root@" + ip

	cmds := [][]string{
		{"ssh", sshTarget, "mkdir", "-p", appDir},
		{"scp", execPath, sshTarget + ":" + appDir + "/"},
		{"scp", zipPath, sshTarget + ":" + appDir + "/"},
		{"ssh", sshTarget, "unzip", "-o", filepath.Join(appDir, filepath.Base(zipPath)), "-d", appDir},
	}

	for _, c := range cmds {
		if out, err := runCmd(c[0], c[1:]...); err != nil {
			jsonResponse(w, http.StatusInternalServerError,
				Response{false, fmt.Sprintf("error ejecutando %v: %s — %s", c, err.Error(), out)})
			return
		}
	}

	// 4. Crear archivo .service de systemd y enviarlo a la VM
	serviceName := filepath.Base(execPath)
	serviceContent := fmt.Sprintf(`[Unit]
Description=Servicio gestionado por daemon-manager (%s)
After=network.target

[Service]
Type=simple
ExecStart=%s/%s
WorkingDirectory=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, serviceName, appDir, serviceName, appDir)

	localServiceFile := filepath.Join(uploadDir, serviceName+".service")
	if err := os.WriteFile(localServiceFile, []byte(serviceContent), 0644); err != nil {
		jsonResponse(w, http.StatusInternalServerError, Response{false, "error al crear .service: " + err.Error()})
		return
	}

	remoteServicePath := fmt.Sprintf("%s:/etc/systemd/system/%s.service", sshTarget, serviceName)
	if out, err := runCmd("scp", localServiceFile, remoteServicePath); err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al copiar .service: " + err.Error() + " — " + out})
		return
	}

	// 5. Recargar systemd y habilitar el servicio en la VM
	reloadCmds := []string{
		fmt.Sprintf("systemctl daemon-reload && systemctl enable %s.service && systemctl start %s.service", serviceName, serviceName),
	}
	if out, err := runCmd("ssh", sshTarget, reloadCmds[0]); err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al activar servicio: " + err.Error() + " — " + out})
		return
	}

	// 6. Convertir disco de la VM plantilla a tipo multiconexión
	out, err := runCmd(vboxManage, "showvminfo", vmTemplate, "--machinereadable")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al obtener info de VM: " + err.Error() + " — " + out})
		return
	}

	// Extraer UUID del disco desde la salida de showvminfo
	diskUUID := extractDiskUUID(out)
	if diskUUID == "" {
		jsonResponse(w, http.StatusInternalServerError, Response{false, "no se encontró UUID del disco de la VM"})
		return
	}

	// Apagar VM para modificar disco
	runCmd(vboxManage, "controlvm", vmTemplate, "poweroff")

	// Clonar disco como multiconexión
	vboxDefaultPath := os.Getenv("HOME") + "/VirtualBox VMs/" + diskName + "/" + diskName + ".vdi"
	os.MkdirAll(filepath.Dir(vboxDefaultPath), 0755)

	if out, err := runCmd(vboxManage, "clonemedium", diskUUID, vboxDefaultPath,
		"--format", "VDI"); err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al clonar disco: " + err.Error() + " — " + out})
		return
	}

	// Convertir a multiconexión
	if out, err := runCmd(vboxManage, "modifymedium", vboxDefaultPath,
		"--type", "multiattach"); err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al convertir a multiconexión: " + err.Error() + " — " + out})
		return
	}

	jsonResponse(w, http.StatusOK, Response{
		true,
		fmt.Sprintf("Demonio '%s' configurado. Disco multiconexión '%s' creado en %s",
			serviceName, diskName, vboxDefaultPath),
	})
}

// extractDiskUUID extrae el UUID del primer disco encontrado en la salida de showvminfo
func extractDiskUUID(vmInfo string) string {
	for _, line := range strings.Split(vmInfo, "\n") {
		// Líneas con formato: "IDE Controller-0-0"="UUID"
		if strings.Contains(line, `"`) && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.Trim(parts[1], `"`)
				if len(uuid) == 36 && strings.Count(uuid, "-") == 4 {
					return uuid
				}
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// Handler: Listar discos multiconexión
// GET /api/disks
// ─────────────────────────────────────────────

func listDisksHandler(w http.ResponseWriter, r *http.Request) {
	out, err := runCmd(vboxManage, "list", "hdds")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, Response{false, "error al listar discos: " + err.Error()})
		return
	}

	var disks []Disk
	var current Disk
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name:") {
			current.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		} else if strings.HasPrefix(line, "Location:") {
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "Location:"))
		} else if strings.HasPrefix(line, "Type:") && strings.Contains(line, "Multiattach") {
			if current.Name != "" {
				disks = append(disks, current)
			}
			current = Disk{}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(disks)
}

// ─────────────────────────────────────────────
// Handler: Crear VM desde disco multiconexión
// POST /api/create-vm
// JSON body: {"vm_name": "...", "disk_name": "...", "port": "..."}
// ─────────────────────────────────────────────

func createVMHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}

	var body struct {
		VMName   string `json:"vm_name"`
		DiskName string `json:"disk_name"`
		Port     string `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VMName == "" || body.DiskName == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "vm_name y disk_name son requeridos"})
		return
	}

	diskPath := os.Getenv("HOME") + "/VirtualBox VMs/" + body.DiskName + "/" + body.DiskName + ".vdi"

	// Crear y registrar la VM
	steps := [][]string{
		{vboxManage, "createvm", "--name", body.VMName, "--ostype", "Debian_64", "--register"},
		{vboxManage, "modifyvm", body.VMName, "--memory", "1024", "--cpus", "1",
			"--nic1", "hostonly", "--hostonlyadapter1", "vboxnet0"},
		{vboxManage, "storagectl", body.VMName, "--name", "SATA Controller", "--add", "sata"},
		{vboxManage, "storageattach", body.VMName, "--storagectl", "SATA Controller",
			"--port", "0", "--device", "0", "--type", "hdd", "--medium", diskPath,
			"--mtype", "multiattach"},
		{vboxManage, "startvm", body.VMName, "--type", "headless"},
	}

	for _, step := range steps {
		if out, err := runCmd(step[0], step[1:]...); err != nil {
			jsonResponse(w, http.StatusInternalServerError,
				Response{false, fmt.Sprintf("error en paso %v: %s — %s", step[1], err.Error(), out)})
			return
		}
	}

	// Obtener IP de la VM (esperar unos segundos a que arranque)
	ip := getVMIP(body.VMName)

	jsonResponse(w, http.StatusOK, map[string]string{
		"success": "true",
		"vm_name": body.VMName,
		"ip":      ip,
		"port":    body.Port,
		"message": "VM creada e iniciada correctamente",
	})
}

// getVMIP obtiene la dirección IP asignada a la VM mediante guestproperty
func getVMIP(vmName string) string {
	out, err := runCmd(vboxManage, "guestproperty", "get", vmName, "/VirtualBox/GuestInfo/Net/0/V4/IP")
	if err != nil {
		return "desconocida"
	}
	parts := strings.SplitN(out, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return "desconocida"
}

// ─────────────────────────────────────────────
// Handler: Listar VMs activas
// GET /api/vms
// ─────────────────────────────────────────────

func listVMsHandler(w http.ResponseWriter, r *http.Request) {
	out, err := runCmd(vboxManage, "list", "runningvms")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, Response{false, "error al listar VMs: " + err.Error()})
		return
	}

	var vms []VirtualMachine
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// formato: "VMName" {uuid}
		parts := strings.SplitN(line, " ", 2)
		name := strings.Trim(parts[0], `"`)
		vm := VirtualMachine{
			Name: name,
			IP:   getVMIP(name),
		}
		vms = append(vms, vm)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vms)
}

// ─────────────────────────────────────────────
// Handler: Iniciar VM apagada
// POST /api/start-vm
// JSON body: {"vm_name": "..."}
// ─────────────────────────────────────────────

func startVMHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}

	var body struct {
		VMName string `json:"vm_name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.VMName == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "vm_name es requerido"})
		return
	}

	out, err := runCmd(vboxManage, "startvm", body.VMName, "--type", "headless")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al iniciar VM: " + err.Error() + " — " + out})
		return
	}

	jsonResponse(w, http.StatusOK, Response{true, "VM " + body.VMName + " iniciada"})
}

// ─────────────────────────────────────────────
// Handler: Apagar VM
// POST /api/poweroff-vm
// JSON body: {"vm_name": "..."}
// ─────────────────────────────────────────────

func powerOffVMHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}

	var body struct {
		VMName string `json:"vm_name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.VMName == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "vm_name es requerido"})
		return
	}

	out, err := runCmd(vboxManage, "controlvm", body.VMName, "poweroff")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al apagar VM: " + err.Error() + " — " + out})
		return
	}

	jsonResponse(w, http.StatusOK, Response{true, "VM " + body.VMName + " apagada"})
}

// ─────────────────────────────────────────────
// Handler: Gestión de servicios con systemctl
// POST /api/service
// JSON body: {"action": "start|stop|restart|enable|disable|status|logs", "service": "nombre"}
// ─────────────────────────────────────────────

func serviceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}

	var body struct {
		Action  string `json:"action"`
		Service string `json:"service"`
		VMHost  string `json:"vm_host"` // IP de la VM destino (opcional, vacío = local)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Service == "" || body.Action == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "action y service son requeridos"})
		return
	}

	validActions := map[string]bool{
		"start": true, "stop": true, "restart": true,
		"enable": true, "disable": true, "status": true, "logs": true,
	}
	if !validActions[body.Action] {
		jsonResponse(w, http.StatusBadRequest, Response{false, "acción inválida: " + body.Action})
		return
	}

	var out string
	var err error

	buildCmd := func() []string {
		if body.Action == "logs" {
			return []string{systemctlBin, "journalctl", "-u", body.Service + ".service", "--no-pager", "-n", "100"}
		}
		return []string{systemctlBin, body.Action, body.Service + ".service"}
	}

	cmdArgs := buildCmd()

	if body.VMHost != "" {
		// Ejecutar en VM remota via SSH
		remote := "root@" + body.VMHost
		remoteCmd := strings.Join(cmdArgs, " ")
		out, err = runCmd("ssh", remote, remoteCmd)
	} else {
		out, err = runCmd(cmdArgs[0], cmdArgs[1:]...)
	}

	if err != nil && body.Action != "status" && body.Action != "logs" {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error ejecutando " + body.Action + ": " + err.Error() + "\n" + out})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ServiceStatus{Service: body.Service, Output: out})
}

// ─────────────────────────────────────────────
// Handler: Eliminar disco multiconexión
// POST /api/delete-disk
// JSON body: {"disk_name": "..."}
// ─────────────────────────────────────────────

func deleteDiskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, Response{false, "método no permitido"})
		return
	}

	var body struct {
		DiskName string `json:"disk_name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.DiskName == "" {
		jsonResponse(w, http.StatusBadRequest, Response{false, "disk_name es requerido"})
		return
	}

	diskPath := os.Getenv("HOME") + "/VirtualBox VMs/" + body.DiskName + "/" + body.DiskName + ".vdi"
	out, err := runCmd(vboxManage, "closemedium", "disk", diskPath, "--delete")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError,
			Response{false, "error al eliminar disco: " + err.Error() + " — " + out})
		return
	}

	jsonResponse(w, http.StatusOK, Response{true, "Disco " + body.DiskName + " eliminado"})
}

// ─────────────────────────────────────────────
// CORS middleware
// ─────────────────────────────────────────────

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

// ─────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────

func main() {
	os.MkdirAll(uploadDir, 0755)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/create-daemon", withCORS(createDaemonHandler))
	mux.HandleFunc("/api/disks", withCORS(listDisksHandler))
	mux.HandleFunc("/api/create-vm", withCORS(createVMHandler))
	mux.HandleFunc("/api/start-vm", withCORS(startVMHandler))
	mux.HandleFunc("/api/vms", withCORS(listVMsHandler))
	mux.HandleFunc("/api/poweroff-vm", withCORS(powerOffVMHandler))
	mux.HandleFunc("/api/service", withCORS(serviceHandler))
	mux.HandleFunc("/api/delete-disk", withCORS(deleteDiskHandler))

	// Servir archivos estáticos (index.html, styles.css, app.js)
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	addr := ":8080"
	log.Printf("Daemon Manager backend iniciado en http://localhost%s\n", addr)
	log.Printf("Frontend disponible en http://localhost%s/\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
