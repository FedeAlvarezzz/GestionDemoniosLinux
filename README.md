# Daemon Manager — Automatización de servicios Linux con systemd

Aplicación web desarrollada en **Golang** para automatizar la configuración de demonios en Linux usando `systemd`. Permite preparar, desplegar y administrar aplicaciones web como servicios en máquinas virtuales gestionadas desde Oracle VirtualBox.

---

## Tabla de contenido

- [Descripción general](#descripción-general)
- [Requisitos previos](#requisitos-previos)
- [Estructura del proyecto](#estructura-del-proyecto)
- [Instalación](#instalación)
- [Uso](#uso)
- [Funcionalidades](#funcionalidades)
- [Arquitectura](#arquitectura)
- [Autores](#autores)

---

## Descripción general

Este proyecto integra conceptos de **virtualización**, **automatización**, **administración de sistemas** y **desarrollo de software**. La aplicación web permite:

1. Subir un ejecutable y configurarlo automáticamente como un servicio `systemd` dentro de una máquina virtual.
2. Convertir el disco de la VM a tipo **multiconexión** para usarlo como plantilla replicable.
3. Crear nuevas máquinas virtuales a partir del disco multiconexión.
4. Gestionar el ciclo de vida del servicio (`start`, `stop`, `restart`, `enable`, `disable`, `status`, `logs`) directamente desde el navegador.

---

## Requisitos previos

| Herramienta | Versión mínima | Notas |
|---|---|---|
| Go | 1.22+ | Backend principal |
| Oracle VirtualBox | 7.0+ | Hypervisor de las VMs |
| GNU/Linux Debian | 13 (Trixie) | SO de las VMs (modo CLI) |
| OpenSSH | Cualquiera | Comunicación host ↔ VM sin contraseña |
| systemd | Cualquiera | Gestión del servicio dentro de la VM |

La comunicación entre la aplicación y las máquinas virtuales se realiza exclusivamente por **SSH con llaves de autenticación** (sin interacción del usuario).

---

## Estructura del proyecto

```
daemon-manager/
├── main.go                  # Punto de entrada de la aplicación
├── go.mod
├── go.sum
├── handlers/
│   ├── daemon.go            # Lógica de creación del demonio
│   ├── disk.go              # Gestión de discos multiconexión
│   └── vm.go                # Gestión de máquinas virtuales
├── ssh/
│   └── client.go            # Cliente SSH para comunicación con VMs
├── systemd/
│   └── template.service     # Plantilla del archivo .service
├── static/
│   ├── index.html           # Interfaz web principal
│   ├── style.css            # Estilos
│   └── app.js               # Lógica del frontend
└── README.md
```

---

## Instalación

### 1. Clonar el repositorio

```bash
git clone https://github.com/usuario/daemon-manager.git
cd daemon-manager
```

### 2. Instalar dependencias de Go

```bash
go mod download
```

### 3. Configurar llaves SSH

Generar un par de llaves para la comunicación sin contraseña con las VMs:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/daemon_manager_key -N ""
```

Copiar la llave pública a la VM plantilla:

```bash
ssh-copy-id -i ~/.ssh/daemon_manager_key.pub usuario@<IP-de-la-VM>
```

### 4. Compilar y ejecutar

```bash
go build -o daemon-manager .
./daemon-manager
```

La aplicación quedará disponible en `http://localhost:8080`.

---

## Uso

### Preparar una máquina virtual

1. Abrir `http://localhost:8080` en el navegador.
2. En el formulario **"Preparar máquina virtual"**, completar:
   - **Archivo ejecutable**: seleccionar el binario de la aplicación web (ej. `servidor-imagenes`).
   - **Número de puerto**: puerto donde la app escuchará peticiones (ej. `8081`).
   - **Archivos adicionales (.zip)**: recursos adicionales que necesite la app.
   - **Máquina virtual (plantilla)**: nombre de la VM base en VirtualBox.
   - **Nombre disco multiconexión**: nombre que tendrá el disco resultante.
3. Presionar **Ejecutar**. La aplicación realizará automáticamente:
   - Copia de los archivos a la VM vía SCP.
   - Creación del archivo `.service` de systemd.
   - Habilitación e inicio del servicio.
   - Conversión del disco a tipo multiconexión.
   - Desacoplamiento del disco de la VM.

### Crear una máquina virtual desde el disco

En el **Tablero de control — Discos Multiconexión**, presionar **"Crear máquina virtual"** e ingresar el nombre deseado. La VM se creará e iniciará automáticamente y aparecerá en el tablero de máquinas virtuales con su IP y puerto.

### Gestionar el servicio

En la sección **"Gestión del servicio (systemctl)"** están disponibles las siguientes acciones:

| Botón | Comando ejecutado |
|---|---|
| Iniciar | `systemctl start <servicio>` |
| Detener | `systemctl stop <servicio>` |
| Reiniciar | `systemctl restart <servicio>` |
| Habilitar | `systemctl enable <servicio>` |
| Deshabilitar | `systemctl disable <servicio>` |
| Ver logs | `journalctl -u <servicio> -n 50` |
| Estado | `systemctl status <servicio>` |

---

## Funcionalidades

- [x] Subida de ejecutable y archivos adicionales (.zip)
- [x] Generación automática del archivo `.service` de systemd
- [x] Despliegue vía SSH sin interacción del usuario
- [x] Conversión de disco a tipo multiconexión en VirtualBox
- [x] Desacoplamiento del disco de la VM plantilla
- [x] Tablero de control de discos multiconexión
- [x] Creación de VMs desde el disco multiconexión
- [x] Inicio automático de la VM recién creada
- [x] Visualización de IP y puerto de la VM en el tablero
- [x] Apagado de VM desde el tablero
- [x] Apertura de la app web en otra pestaña del navegador
- [x] Control completo del servicio via `systemctl` desde la UI
- [x] Visualización de logs en tiempo real

---

## Arquitectura

```
[Navegador]
     │  HTTP
     ▼
[Aplicación Go :8080]
     │  SSH (llaves, sin contraseña)
     ▼
[VM con Debian 13 CLI]
     │
     ├── /etc/systemd/system/<app>.service
     ├── /opt/<app>/<ejecutable>
     └── /opt/<app>/recursos/
```

La aplicación Go actúa como orquestador: recibe las instrucciones del usuario desde el navegador, las ejecuta remotamente en la VM a través de SSH, y refleja el estado resultante en la interfaz web.

---

## Autores

Proyecto desarrollado para **Federico Alvarez, Juan Camilo Cuenca, Diego Alexander Jimenez** - Computación en la Nube — Universidad del Quindío, 2026-1.

