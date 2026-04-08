// ─────────────────────────────────────────────
// Configuración
// ─────────────────────────────────────────────

const API = "http://localhost:8080/api";

// ─────────────────────────────────────────────
// Utilidades
// ─────────────────────────────────────────────

function log(msg, isError = false) {
  const area = document.getElementById("log-area");
  const line = document.createElement("div");
  line.style.color = isError ? "#fc8181" : "#e2e8f0";
  line.textContent = msg;
  area.appendChild(line);
  area.scrollTop = area.scrollHeight;
}

function logCmd(cmd, output) {
  const area = document.getElementById("log-area");
  area.innerHTML = `
    <span class="log-prompt">$</span>
    <span style="color:#90cdf4">systemctl</span> ${cmd}<br>
    <span style="color:#e2e8f0;white-space:pre-wrap">${output || "— Sin salida"}</span>
  `;
}

function setLoading(btn, loading) {
  btn.disabled = loading;
  btn.dataset.originalText = btn.dataset.originalText || btn.textContent;
  btn.textContent = loading ? "..." : btn.dataset.originalText;
}

// ─────────────────────────────────────────────
// Sección: Preparar máquina virtual → Ejecutar
// ─────────────────────────────────────────────

document.getElementById("btn-ejecutar").addEventListener("click", async () => {
  const btn = document.getElementById("btn-ejecutar");

  const execFile  = document.getElementById("input-executable").files[0];
  const port      = document.getElementById("input-port").value.trim();
  const zipFile   = document.getElementById("input-zip").files[0];
  const vmTemplate = document.getElementById("input-vm-template").value.trim();
  const diskName  = document.getElementById("input-disk-name").value.trim();

  if (!execFile || !port || !zipFile || !vmTemplate || !diskName) {
    alert("Completa todos los campos antes de ejecutar.");
    return;
  }

  const form = new FormData();
  form.append("executable",   execFile);
  form.append("port",         port);
  form.append("zipfile",      zipFile);
  form.append("vm_template",  vmTemplate);
  form.append("disk_name",    diskName);

  setLoading(btn, true);
  try {
    const res  = await fetch(`${API}/create-daemon`, { method: "POST", body: form });
    const data = await res.json();
    if (data.success) {
      log("✔ " + data.message);
      await loadDisks();
    } else {
      log("✘ " + data.message, true);
    }
  } catch (e) {
    log("✘ Error de conexión: " + e.message, true);
  } finally {
    setLoading(btn, false);
  }
});

// ─────────────────────────────────────────────
// Botones "Explorar" → abren file picker
// ─────────────────────────────────────────────

document.getElementById("btn-explore-exec").addEventListener("click", () =>
  document.getElementById("input-executable").click());

document.getElementById("btn-explore-zip").addEventListener("click", () =>
  document.getElementById("input-zip").click());

// Mostrar nombre del archivo seleccionado en el input de texto visible
document.getElementById("input-executable").addEventListener("change", (e) => {
  document.getElementById("text-executable").value = e.target.files[0]?.name || "";
});
document.getElementById("input-zip").addEventListener("change", (e) => {
  document.getElementById("text-zip").value = e.target.files[0]?.name || "";
});

// ─────────────────────────────────────────────
// Flechas del puerto
// ─────────────────────────────────────────────

document.getElementById("btn-port-up").addEventListener("click", () => {
  const input = document.getElementById("input-port");
  input.value = parseInt(input.value || 8080) + 1;
});
document.getElementById("btn-port-down").addEventListener("click", () => {
  const input = document.getElementById("input-port");
  const val = parseInt(input.value || 8080) - 1;
  input.value = val > 0 ? val : 1;
});

// ─────────────────────────────────────────────
// Cargar tablero de discos multiconexión
// ─────────────────────────────────────────────

async function loadDisks() {
  try {
    const res  = await fetch(`${API}/disks`);
    const data = await res.json();
    const tbody = document.getElementById("disks-tbody");
    tbody.innerHTML = "";

    if (!data || data.length === 0) {
      tbody.innerHTML = `<tr><td colspan="3" style="text-align:center;color:#a0aec0">Sin discos multiconexión</td></tr>`;
      return;
    }

    for (const disk of data) {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><span class="disk-badge">${disk.name}</span></td>
        <td style="font-family:var(--font-mono);font-size:12px;color:#553c9a">${disk.path}</td>
        <td>
          <div class="td-actions">
            <button class="btn btn-sm btn-blue" data-disk="${disk.name}">Crear máquina virtual</button>
            <button class="btn btn-sm btn-red"  data-disk="${disk.name}">Eliminar disco</button>
          </div>
        </td>
      `;
      tbody.appendChild(tr);
    }

    // Botón: Crear VM desde disco
    tbody.querySelectorAll(".btn-blue[data-disk]").forEach(btn => {
      btn.addEventListener("click", () => openCreateVMModal(btn.dataset.disk));
    });

    // Botón: Eliminar disco
    tbody.querySelectorAll(".btn-red[data-disk]").forEach(btn => {
      btn.addEventListener("click", () => deleteDisk(btn.dataset.disk, btn));
    });

  } catch (e) {
    console.error("Error cargando discos:", e);
  }
}

// ─────────────────────────────────────────────
// Modal: Crear VM
// ─────────────────────────────────────────────

function openCreateVMModal(diskName) {
  const vmName = prompt(`Nombre para la nueva máquina virtual\n(Disco: ${diskName}):`);
  if (!vmName) return;
  const port = prompt("Puerto donde escucha la aplicación web:", "8081");
  if (!port) return;
  createVM(vmName.trim(), diskName, port.trim());
}

async function createVM(vmName, diskName, port) {
  try {
    const res  = await fetch(`${API}/create-vm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ vm_name: vmName, disk_name: diskName, port }),
    });
    const data = await res.json();
    if (data.success === "true" || data.success === true) {
      log(`✔ VM '${vmName}' creada — IP: ${data.ip} Puerto: ${data.port}`);
      await loadVMs();
    } else {
      log("✘ " + data.message, true);
    }
  } catch (e) {
    log("✘ Error al crear VM: " + e.message, true);
  }
}

// ─────────────────────────────────────────────
// Eliminar disco
// ─────────────────────────────────────────────

async function deleteDisk(diskName, btn) {
  if (!confirm(`¿Eliminar el disco '${diskName}'? Esta acción no se puede deshacer.`)) return;
  setLoading(btn, true);
  try {
    const res  = await fetch(`${API}/delete-disk`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ disk_name: diskName }),
    });
    const data = await res.json();
    log(data.success ? `✔ ${data.message}` : `✘ ${data.message}`, !data.success);
    await loadDisks();
  } catch (e) {
    log("✘ Error al eliminar disco: " + e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

// ─────────────────────────────────────────────
// Cargar tablero de máquinas virtuales
// ─────────────────────────────────────────────

async function loadVMs() {
  try {
    const res  = await fetch(`${API}/vms`);
    const data = await res.json();
    const tbody = document.getElementById("vms-tbody");
    tbody.innerHTML = "";

    if (!data || data.length === 0) {
      tbody.innerHTML = `<tr><td colspan="4" style="text-align:center;color:#a0aec0">Sin máquinas virtuales activas</td></tr>`;
      return;
    }

    for (const vm of data) {
      const tr = document.createElement("tr");
      const appUrl = `http://${vm.ip}:${vm.port || "8081"}`;
      tr.innerHTML = `
        <td style="font-weight:600">${vm.name}</td>
        <td><span class="ip-badge">${vm.ip}</span></td>
        <td><span class="port-badge">${vm.port || "—"}</span></td>
        <td>
          <div class="td-actions">
            <button class="btn btn-sm btn-green" data-vm="${vm.name}" data-action="start">Iniciar</button>
            <button class="btn btn-sm btn-red"   data-vm="${vm.name}" data-action="poweroff">Apagar</button>
            <a class="btn btn-sm btn-teal" href="${appUrl}" target="_blank" rel="noopener">Ir →</a>
          </div>
        </td>
      `;
      tbody.appendChild(tr);
    }

    // Botón: Iniciar VM
    tbody.querySelectorAll(".btn-green[data-vm]").forEach(btn => {
      btn.addEventListener("click", () => startVM(btn.dataset.vm, btn));
    });

    // Botón: Apagar VM
    tbody.querySelectorAll(".btn-red[data-vm]").forEach(btn => {
      btn.addEventListener("click", () => powerOffVM(btn.dataset.vm, btn));
    });

  } catch (e) {
    console.error("Error cargando VMs:", e);
  }
}

async function startVM(vmName, btn) {
  setLoading(btn, true);
  try {
    const res  = await fetch(`${API}/start-vm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ vm_name: vmName }),
    });
    const data = await res.json();
    log(data.success ? `✔ ${data.message}` : `✘ ${data.message}`, !data.success);
    await loadVMs();
  } catch (e) {
    log("✘ Error al iniciar VM: " + e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

async function powerOffVM(vmName, btn) {
  if (!confirm(`¿Apagar la VM '${vmName}'?`)) return;
  setLoading(btn, true);
  try {
    const res  = await fetch(`${API}/poweroff-vm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ vm_name: vmName }),
    });
    const data = await res.json();
    log(data.success ? `✔ ${data.message}` : `✘ ${data.message}`, !data.success);
    await loadVMs();
  } catch (e) {
    log("✘ Error al apagar VM: " + e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

// ─────────────────────────────────────────────
// Gestión del servicio (systemctl)
// ─────────────────────────────────────────────

async function callService(action) {
  const service = document.getElementById("input-service-name").value.trim();
  const vmHost  = document.getElementById("input-service-vm").value.trim();

  if (!service) {
    alert("Escribe el nombre del servicio.");
    return;
  }

  const cmdLabel = action === "logs"
    ? `journalctl -u ${service}.service -n 100`
    : `${action} ${service}.service`;

  logCmd(cmdLabel, "Ejecutando...");

  try {
    const res  = await fetch(`${API}/service`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action, service, vm_host: vmHost }),
    });
    const data = await res.json();
    logCmd(cmdLabel, data.output || data.message || "OK");
  } catch (e) {
    logCmd(cmdLabel, "Error de conexión: " + e.message);
  }
}

// Conectar botones de systemctl
document.getElementById("btn-start").addEventListener("click",    () => callService("start"));
document.getElementById("btn-stop").addEventListener("click",     () => callService("stop"));
document.getElementById("btn-restart").addEventListener("click",  () => callService("restart"));
document.getElementById("btn-enable").addEventListener("click",   () => callService("enable"));
document.getElementById("btn-disable").addEventListener("click",  () => callService("disable"));
document.getElementById("btn-logs").addEventListener("click",     () => callService("logs"));
document.getElementById("btn-status").addEventListener("click",   () => callService("status"));

// ─────────────────────────────────────────────
// Inicialización
// ─────────────────────────────────────────────

document.addEventListener("DOMContentLoaded", () => {
  loadDisks();
  loadVMs();
  // Refrescar cada 15 segundos
  setInterval(loadVMs, 15000);
});