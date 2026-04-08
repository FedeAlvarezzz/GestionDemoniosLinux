// script.js

const logArea = document.getElementById("logArea");
const selectorServicio = document.getElementById("selectorServicio");
const puertoInput = document.getElementById("puerto");
const ejecutableInput = document.getElementById("ejecutablePath");
const zipInput = document.getElementById("zipPath");
const plantillaInput = document.getElementById("plantillaVM");
const discoInput = document.getElementById("discoNombre");

const API = "";

// Funciones de logging
function log(msg) {
    const span = document.createElement("div");
    span.textContent = msg;
    logArea.appendChild(span);
    logArea.scrollTop = logArea.scrollHeight;
}

// Cambiar puerto con flechas
function cambiarPuerto(delta) {
    let val = parseInt(puertoInput.value) || 8081;
    val += delta;
    if (val < 1024) val = 1024;
    if (val > 65535) val = 65535;
    puertoInput.value = val;
}

// Seleccionar archivo (dummy, solo placeholder)
function seleccionarArchivo(tipo) {
    alert("En esta demo debes escribir la ruta manualmente para " + tipo);
}

// Crear demonio
async function crearDemonio() {
    const payload = {
        ejecutable: ejecutableInput.value,
        puerto: puertoInput.value,
        zip: zipInput.value,
        plantilla: plantillaInput.value,
        disco: discoInput.value
    };

    try {
        const res = await fetch("/crear-demonio", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload)
        });
        const text = await res.text();
        log(text);
        actualizarDiscos();
    } catch (err) {
        console.error(err);
        log("Error al crear demonio");
    }
}

// Actualizar lista de discos
async function actualizarDiscos() {
    try {
        const res = await fetch("/discos");
        let discos = await res.json();
        if (!Array.isArray(discos)) discos = [];

        const tbody = document.getElementById("listaDiscos");
        tbody.innerHTML = "";

        discos.forEach(d => {
            const tr = document.createElement("tr");
            tr.innerHTML = `
                <td>${d}</td>
                <td>${d}.vdi</td>
                <td>
                    <button onclick="crearVMPrompt('${d}')">Crear VM</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (err) {
        console.error(err);
        log("Error al cargar discos");
    }
}

// Crear VM con prompt de nombre
function crearVMPrompt(disco) {
    const nombre = prompt("Nombre de la nueva VM:");
    if (!nombre) return;
    crearVM(nombre, disco, puertoInput.value);
}

// Crear VM
async function crearVM(nombre, disco, puerto) {
    try {
        const res = await fetch(`/crear-vm?nombre=${nombre}&disco=${disco}&puerto=${puerto}`);
        const vm = await res.json();
        log(`VM ${vm.Nombre} creada con IP ${vm.IP} en puerto ${vm.Puerto}`);
        actualizarVMs();
    } catch (err) {
        console.error(err);
        log("Error al crear VM");
    }
}

// Actualizar lista de VMs
async function actualizarVMs() {
    try {
        const res = await fetch("/vms");
        let vms = await res.json();
        if (!Array.isArray(vms)) vms = [];

        const tbody = document.getElementById("listaVMs");
        tbody.innerHTML = "";

        vms.forEach(vm => {
            const tr = document.createElement("tr");
            tr.innerHTML = `
                <td>${vm.Nombre}</td>
                <td>${vm.IP}</td>
                <td>${vm.Puerto}</td>
                <td>
                    <button onclick="apagarVM('${vm.Nombre}')">Apagar</button>
                    <button onclick="abrirApp('${vm.IP}', '${vm.Puerto}')">Abrir App</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (err) {
        console.error(err);
        log("Error al cargar VMs");
    }
}

// Apagar VM
async function apagarVM(nombre) {
    try {
        await fetch(`/apagar-vm?nombre=${nombre}`);
        log(`VM ${nombre} apagada`);
        actualizarVMs();
    } catch (err) {
        console.error(err);
        log("Error al apagar VM");
    }
}

// Abrir aplicación web de la VM
function abrirApp(ip, puerto) {
    window.open(`http://${ip}:${puerto}`, "_blank");
}

// Control de servicio (systemctl)
async function controlServicio(cmd) {
    log(`Comando systemctl: ${cmd} (pendiente de backend)`);
    // Aquí se llamaría a un endpoint de Go si existiera
}

// Ver logs
function verLogs() {
    log("Ver logs (pendiente de backend)");
}

// Ver status
function verStatus() {
    log("Ver status (pendiente de backend)");
}

// Inicializar
document.addEventListener("DOMContentLoaded", () => {
    actualizarDiscos();
    actualizarVMs();
});