function seleccionarArchivo(tipo){
    alert("Para el parcial escribe la ruta manualmente.\nEjemplo:\n/home/diego/appweb");
}

function cambiarPuerto(valor){

let puerto = document.getElementById("puerto")

let nuevo = parseInt(puerto.value) + valor

if(nuevo >= 1024 && nuevo <= 65535){
puerto.value = nuevo
}

}

function log(msg){

let area = document.getElementById("logArea")

area.innerHTML += "<br>$ " + msg

area.scrollTop = area.scrollHeight

}

async function crearDemonio(){

let data = {

ejecutable: document.getElementById("ejecutablePath").value,
puerto: document.getElementById("puerto").value,
zip: document.getElementById("zipPath").value,
plantilla: document.getElementById("plantillaVM").value,
disco: document.getElementById("discoNombre").value

}

log("Creando demonio...")

let res = await fetch("/crear-demonio",{

method:"POST",
headers:{"Content-Type":"application/json"},
body:JSON.stringify(data)

})

let txt = await res.text()

log(txt)

cargarDiscos()

}

async function cargarDiscos(){

let res = await fetch("/discos")

let discos = await res.json()

let tabla = document.getElementById("listaDiscos")

tabla.innerHTML=""

discos.forEach(d=>{

tabla.innerHTML += `
<tr>
<td>${d}</td>
<td>${d}.vdi</td>
<td>
<button onclick="crearVM('${d}')">Crear VM</button>
</td>
</tr>
`

})

}

async function crearVM(disco){

let nombre = prompt("Nombre de la VM")

if(!nombre) return

let puerto = document.getElementById("puerto").value

log("Creando VM " + nombre)

await fetch(`/crear-vm?nombre=${nombre}&disco=${disco}&puerto=${puerto}`)

cargarVMs()

}

async function cargarVMs(){

let res = await fetch("/vms")

let vms = await res.json()

let tabla = document.getElementById("listaVMs")

tabla.innerHTML=""

vms.forEach(vm=>{

tabla.innerHTML += `
<tr>
<td>${vm.Nombre}</td>
<td>${vm.IP}</td>
<td>${vm.Puerto}</td>
<td>
<button onclick="abrirApp('${vm.IP}','${vm.Puerto}')">Abrir</button>
<button onclick="apagarVM('${vm.Nombre}')">Apagar</button>
</td>
</tr>
`

})

}

function abrirApp(ip,puerto){

let url = "http://" + ip + ":" + puerto

log("Abriendo " + url)

window.open(url,"_blank")

}

async function apagarVM(nombre){

log("Apagando VM " + nombre)

await fetch(`/apagar-vm?nombre=${nombre}`)

cargarVMs()

}

async function controlServicio(accion){

let servicio = document.getElementById("selectorServicio").value

log("systemctl " + accion + " " + servicio)

let res = await fetch("/servicio",{

method:"POST",
headers:{"Content-Type":"application/json"},
body:JSON.stringify({

accion:accion,
servicio:servicio

})

})

let txt = await res.text()

log(txt)

}

async function verLogs(){

let servicio = document.getElementById("selectorServicio").value

let res = await fetch("/logs?servicio="+servicio)

let txt = await res.text()

log(txt)

}

async function verStatus(){

let servicio = document.getElementById("selectorServicio").value

let res = await fetch("/status?servicio="+servicio)

let txt = await res.text()

log(txt)

}

async function cargarServicios(){

let res = await fetch("/servicios")

let servicios = await res.json()

let selector = document.getElementById("selectorServicio")

selector.innerHTML=""

servicios.forEach(s=>{

let opt = document.createElement("option")

opt.value = s

opt.text = s

selector.appendChild(opt)

})

}

window.onload=function(){

cargarDiscos()
cargarVMs()
cargarServicios()

}