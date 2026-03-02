// gem2api Admin Panel
(() => {
	let authToken = localStorage.getItem("gem2api_token") || "";
	const API = "";
	let viewerWs = null;
	let viewerProfileId = null;

	// Elements
	const loginSection = document.getElementById("login-section");
	const dashSection = document.getElementById("dashboard-section");
	const loginForm = document.getElementById("login-form");
	const loginError = document.getElementById("login-error");
	const addForm = document.getElementById("add-form");
	const accountsBody = document.getElementById("accounts-body");
	const noAccounts = document.getElementById("no-accounts");
	const statsDisplay = document.getElementById("stats-display");
	const logoutBtn = document.getElementById("logout-btn");

	// Browser elements
	const browserSection = document.getElementById("browser-section");
	const addProfileForm = document.getElementById("add-profile-form");
	const profilesBody = document.getElementById("profiles-body");
	const noProfiles = document.getElementById("no-profiles");
	const viewerModal = document.getElementById("viewer-modal");
	const viewerCanvas = document.getElementById("viewer-canvas");
	const viewerStatus = document.getElementById("viewer-status");
	const viewerTitle = document.getElementById("viewer-title");

	async function api(method, path, body) {
		const opts = { method, headers: { "Content-Type": "application/json" } };
		if (authToken) opts.headers["Authorization"] = "Bearer " + authToken;
		if (body) opts.body = JSON.stringify(body);
		const resp = await fetch(API + path, opts);
		if (resp.status === 401) {
			logout();
			throw new Error("Unauthorized");
		}
		const data = await resp.json();
		if (!resp.ok) throw new Error(data.error || "Request failed");
		return data;
	}

	function showDashboard() {
		loginSection.style.display = "none";
		dashSection.style.display = "block";
		loadAccounts();
		loadStats();
		loadConfig();
		checkBrowserEnabled();
	}

	function showLogin() {
		loginSection.style.display = "flex";
		dashSection.style.display = "none";
	}

	function logout() {
		authToken = "";
		localStorage.removeItem("gem2api_token");
		showLogin();
	}

	// Login
	loginForm.addEventListener("submit", async (e) => {
		e.preventDefault();
		loginError.textContent = "";
		try {
			const data = await api("POST", "/api/admin/login", {
				username: document.getElementById("username").value,
				password: document.getElementById("password").value,
			});
			authToken = data.token;
			localStorage.setItem("gem2api_token", authToken);
			showDashboard();
		} catch (err) {
			loginError.textContent = err.message;
		}
	});

	logoutBtn.addEventListener("click", logout);

	// Load accounts
	async function loadAccounts() {
		try {
			const accounts = await api("GET", "/api/admin/cookies");
			accountsBody.innerHTML = "";
			if (!accounts || accounts.length === 0) {
				noAccounts.style.display = "block";
				document.getElementById("accounts-table").style.display = "none";
				return;
			}
			noAccounts.style.display = "none";
			document.getElementById("accounts-table").style.display = "table";
			accounts.forEach((a) => {
				const status = a.banned_at
					? "banned"
					: a.is_active
						? "active"
						: "disabled";
				const statusClass = "status-" + status;
				const tr = document.createElement("tr");
				tr.innerHTML = `
                    <td>${a.id}</td>
                    <td>${a.nickname || "-"}</td>
                    <td><code>${a.psid_prefix}</code></td>
                    <td><span class="${statusClass}">${status}</span></td>
                    <td>${a.use_count}</td>
                    <td>${a.error_count} (${a.consecutive_errors} consec)</td>
                    <td>${a.last_used_at || "never"}</td>
                    <td class="actions">
                        ${
													a.is_active
														? `<button class="btn-small btn-secondary" onclick="disableAccount(${a.id})">Disable</button>`
														: `<button class="btn-small" onclick="enableAccount(${a.id})">Enable</button>`
												}
                        <button class="btn-small btn-secondary" onclick="testAccount(${a.id})">Test</button>
                        <button class="btn-small btn-danger" onclick="deleteAccount(${a.id})">Delete</button>
                    </td>
                `;
				accountsBody.appendChild(tr);
			});
		} catch (err) {
			console.error("Failed to load accounts:", err);
		}
	}

	async function loadStats() {
		try {
			const s = await api("GET", "/api/admin/stats");
			statsDisplay.textContent = `${s.active_accounts}/${s.total_accounts} active`;
		} catch (err) {
			statsDisplay.textContent = "error";
		}
	}

	// Add account
	addForm.addEventListener("submit", async (e) => {
		e.preventDefault();
		try {
			await api("POST", "/api/admin/cookies", {
				secure_1psid: document.getElementById("add-psid").value,
				secure_1psidts: document.getElementById("add-psidts").value,
				nickname: document.getElementById("add-nickname").value,
			});
			addForm.reset();
			loadAccounts();
			loadStats();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	});

	// Global action functions
	window.enableAccount = async (id) => {
		try {
			await api("POST", `/api/admin/cookies/${id}/enable`);
			loadAccounts();
			loadStats();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};
	window.disableAccount = async (id) => {
		try {
			await api("POST", `/api/admin/cookies/${id}/disable`);
			loadAccounts();
			loadStats();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};
	window.deleteAccount = async (id) => {
		if (!confirm("Delete account " + id + "?")) return;
		try {
			await api("DELETE", `/api/admin/cookies/${id}`);
			loadAccounts();
			loadStats();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.testAccount = async (id) => {
		const btn = event.target;
		const orig = btn.textContent;
		btn.textContent = "Testing...";
		btn.disabled = true;
		try {
			const data = await api("POST", `/api/admin/cookies/${id}/test`);
			if (data.valid) {
				alert(`Account #${id}: Cookies are VALID`);
			} else {
				alert(
					`Account #${id}: Cookies INVALID\n${data.error || "Unknown error"}`,
				);
			}
		} catch (err) {
			alert("Test failed: " + err.message);
		} finally {
			btn.textContent = orig;
			btn.disabled = false;
		}
	};

	// ==========================================
	// Configuration Management
	// ==========================================

	const configContainer = document.getElementById("config-container");

	async function loadConfig() {
		try {
			const data = await api("GET", "/api/admin/config");
			configContainer.innerHTML = "";
			const keys = Object.keys(data).sort();
			if (keys.length === 0) {
				configContainer.innerHTML =
					'<p style="color: #888">No config entries.</p>';
				return;
			}
			keys.forEach((key) => {
				const row = document.createElement("div");
				row.className = "config-row";
				row.innerHTML = `
					<label>${escapeHtml(key)}</label>
					<input type="text" data-config-key="${escapeHtml(key)}" value="${escapeHtml(data[key])}" />
				`;
				configContainer.appendChild(row);
			});
			// Add new key row
			const addRow = document.createElement("div");
			addRow.className = "config-row";
			addRow.innerHTML = `
				<input type="text" id="config-new-key" placeholder="New key" />
				<input type="text" id="config-new-value" placeholder="New value" />
				<button class="btn-small" onclick="addConfigKey()">Add</button>
			`;
			configContainer.appendChild(addRow);
		} catch (err) {
			configContainer.innerHTML =
				'<p style="color: #f85149">Failed to load config</p>';
		}
	}

	window.loadConfig = loadConfig;

	window.saveConfig = async () => {
		const inputs = configContainer.querySelectorAll("input[data-config-key]");
		const updates = {};
		inputs.forEach((input) => {
			updates[input.dataset.configKey] = input.value;
		});
		try {
			await api("PUT", "/api/admin/config", updates);
			alert("Config saved");
			loadConfig();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.addConfigKey = async () => {
		const key = document.getElementById("config-new-key").value.trim();
		const value = document.getElementById("config-new-value").value;
		if (!key) {
			alert("Key is required");
			return;
		}
		try {
			await api("PUT", "/api/admin/config", { [key]: value });
			loadConfig();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	// ==========================================
	// Browser Profile Management
	// ==========================================
	// Browser Profile Management
	// ==========================================

	async function checkBrowserEnabled() {
		try {
			await api("GET", "/api/admin/browser/profiles");
			browserSection.style.display = "block";
			loadProfiles();
		} catch (err) {
			// Browser feature not enabled — hide section
			browserSection.style.display = "none";
		}
	}

	async function loadProfiles() {
		try {
			const data = await api("GET", "/api/admin/browser/profiles");
			const profiles = data.profiles || [];
			profilesBody.innerHTML = "";
			if (profiles.length === 0) {
				noProfiles.style.display = "block";
				document.getElementById("profiles-table").style.display = "none";
				return;
			}
			noProfiles.style.display = "none";
			document.getElementById("profiles-table").style.display = "table";
			profiles.forEach((p) => {
				const statusClass = "status-" + p.status;
				const accountText = p.account_id ? `#${p.account_id}` : "-";
				const lastRefresh = p.last_refresh
					? new Date(p.last_refresh).toLocaleString()
					: "never";
				const lastError = p.last_error
					? `<span class="truncate" title="${escapeHtml(p.last_error)}">${escapeHtml(p.last_error)}</span>`
					: "-";

				const tr = document.createElement("tr");
				tr.innerHTML = `
					<td>${p.id}</td>
					<td>${escapeHtml(p.profile_name)}</td>
					<td><span class="${statusClass}">${p.status}</span></td>
					<td>${accountText}</td>
					<td>${lastRefresh}</td>
					<td>${lastError}</td>
					<td class="actions">${profileActions(p)}</td>
				`;
				profilesBody.appendChild(tr);
			});
		} catch (err) {
			console.error("Failed to load profiles:", err);
		}
	}

	function profileActions(p) {
		const btns = [];
		if (p.status === "pending" || p.status === "error") {
			btns.push(
				`<button class="btn-small" onclick="startLogin(${p.id})">Login</button>`,
			);
		}
		if (p.status === "logging_in") {
			btns.push(
				`<button class="btn-small" onclick="openViewer(${p.id}, '${escapeHtml(p.profile_name)}')">View</button>`,
			);
			btns.push(
				`<button class="btn-small btn-secondary" onclick="cancelLogin(${p.id})">Cancel</button>`,
			);
		}
		if (p.status === "active") {
			btns.push(
				`<button class="btn-small btn-secondary" onclick="refreshProfile(${p.id})">Refresh</button>`,
			);
		}
		btns.push(
			`<button class="btn-small btn-danger" onclick="deleteProfile(${p.id})">Delete</button>`,
		);
		return btns.join("");
	}

	function escapeHtml(str) {
		const div = document.createElement("div");
		div.textContent = str;
		return div.innerHTML;
	}

	// Add profile
	if (addProfileForm) {
		addProfileForm.addEventListener("submit", async (e) => {
			e.preventDefault();
			try {
				await api("POST", "/api/admin/browser/profiles", {
					name: document.getElementById("profile-name").value,
				});
				addProfileForm.reset();
				loadProfiles();
			} catch (err) {
				alert("Failed: " + err.message);
			}
		});
	}

	window.startLogin = async (id) => {
		try {
			const data = await api("POST", `/api/admin/browser/profiles/${id}/login`);
			loadProfiles();
			// Auto-open viewer
			const profiles = (
				await api("GET", "/api/admin/browser/profiles")
			).profiles.find((p) => p.id === id);
			const name = profiles ? profiles.profile_name : "Profile " + id;
			openViewerWs(id, name);
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.cancelLogin = async (id) => {
		try {
			await api("POST", `/api/admin/browser/profiles/${id}/cancel`);
			loadProfiles();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.finishLogin = async () => {
		if (!viewerProfileId) return;
		try {
			const data = await api(
				"POST",
				`/api/admin/browser/profiles/${viewerProfileId}/finish`,
			);
			closeViewer();
			loadProfiles();
			loadAccounts();
			loadStats();
			alert(
				data.message +
					(data.has_psid ? " (PSID extracted)" : " (no PSID found)"),
			);
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.refreshProfile = async (id) => {
		try {
			await api("POST", `/api/admin/browser/profiles/${id}/refresh`);
			loadProfiles();
			loadAccounts();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	window.deleteProfile = async (id) => {
		if (!confirm("Delete browser profile " + id + "?")) return;
		try {
			await api("DELETE", `/api/admin/browser/profiles/${id}`);
			loadProfiles();
		} catch (err) {
			alert("Failed: " + err.message);
		}
	};

	// ==========================================
	// WebSocket Screencast Viewer
	// ==========================================

	window.openViewer = (id, name) => {
		openViewerWs(id, name);
	};

	function openViewerWs(profileId, name) {
		viewerProfileId = profileId;
		viewerTitle.textContent = "Browser Login — " + name;
		viewerStatus.textContent = "Connecting...";
		viewerStatus.style.display = "block";
		viewerModal.style.display = "flex";

		const proto = location.protocol === "https:" ? "wss:" : "ws:";
		const url =
			proto +
			"//" +
			location.host +
			`/api/admin/browser/profiles/${profileId}/view`;

		// Add auth token as query param since WebSocket doesn't support custom headers
		const wsUrl = url + "?token=" + encodeURIComponent(authToken);

		if (viewerWs) {
			viewerWs.close();
		}

		viewerWs = new WebSocket(wsUrl);

		viewerWs.onopen = () => {
			viewerStatus.style.display = "none";
			setupCanvasEvents();
		};

		viewerWs.onmessage = (evt) => {
			try {
				const msg = JSON.parse(evt.data);
				if (msg.type === "frame") {
					drawFrame(msg.data);
				}
			} catch (e) {
				console.error("Frame parse error:", e);
			}
		};

		viewerWs.onclose = () => {
			viewerStatus.textContent = "Disconnected";
			viewerStatus.style.display = "block";
		};

		viewerWs.onerror = () => {
			viewerStatus.textContent = "Connection error";
			viewerStatus.style.display = "block";
		};
	}

	window.cancelViewer = () => {
		if (viewerProfileId) {
			api("POST", `/api/admin/browser/profiles/${viewerProfileId}/cancel`)
				.then(() => loadProfiles())
				.catch(() => {});
		}
		closeViewer();
	};

	function closeViewer() {
		if (viewerWs) {
			viewerWs.close();
			viewerWs = null;
		}
		viewerProfileId = null;
		viewerModal.style.display = "none";
	}

	// Draw base64 JPEG frame onto canvas
	const frameImg = new Image();
	frameImg.onload = () => {
		const ctx = viewerCanvas.getContext("2d");
		viewerCanvas.width = frameImg.naturalWidth;
		viewerCanvas.height = frameImg.naturalHeight;
		ctx.drawImage(frameImg, 0, 0);
	};

	function drawFrame(base64Data) {
		frameImg.src = "data:image/jpeg;base64," + base64Data;
	}

	// Canvas coordinate mapping
	function canvasCoords(e) {
		const rect = viewerCanvas.getBoundingClientRect();
		const scaleX = viewerCanvas.width / rect.width;
		const scaleY = viewerCanvas.height / rect.height;
		return {
			x: (e.clientX - rect.left) * scaleX,
			y: (e.clientY - rect.top) * scaleY,
		};
	}

	function mouseButton(b) {
		return b === 2 ? "right" : b === 1 ? "middle" : "left";
	}

	function sendInput(ev) {
		if (viewerWs && viewerWs.readyState === WebSocket.OPEN) {
			viewerWs.send(JSON.stringify(ev));
		}
	}

	function setupCanvasEvents() {
		// Mouse events
		viewerCanvas.onmousedown = (e) => {
			e.preventDefault();
			const c = canvasCoords(e);
			sendInput({
				type: "mousedown",
				x: c.x,
				y: c.y,
				button: mouseButton(e.button),
			});
		};

		viewerCanvas.onmouseup = (e) => {
			e.preventDefault();
			const c = canvasCoords(e);
			sendInput({
				type: "mouseup",
				x: c.x,
				y: c.y,
				button: mouseButton(e.button),
			});
		};

		viewerCanvas.onmousemove = (e) => {
			const c = canvasCoords(e);
			sendInput({ type: "mousemove", x: c.x, y: c.y });
		};

		viewerCanvas.onclick = (e) => {
			e.preventDefault();
			const c = canvasCoords(e);
			sendInput({
				type: "click",
				x: c.x,
				y: c.y,
				button: mouseButton(e.button),
			});
		};

		// Keyboard events — capture when modal is open
		document.addEventListener("keydown", onKeyDown);
		document.addEventListener("keyup", onKeyUp);

		// Scroll
		viewerCanvas.onwheel = (e) => {
			e.preventDefault();
			const c = canvasCoords(e);
			sendInput({
				type: "scroll",
				x: c.x,
				y: c.y,
				deltaX: e.deltaX,
				deltaY: e.deltaY,
			});
		};

		// Prevent context menu
		viewerCanvas.oncontextmenu = (e) => e.preventDefault();
	}

	function onKeyDown(e) {
		if (viewerModal.style.display === "none") return;
		// Allow Escape to close
		if (e.key === "Escape") {
			closeViewer();
			return;
		}
		e.preventDefault();
		if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
			// Printable character — use type event for text input
			sendInput({ type: "type", text: e.key });
		} else {
			sendInput({ type: "keydown", key: e.key });
		}
	}

	function onKeyUp(e) {
		if (viewerModal.style.display === "none") return;
		if (e.key === "Escape") return;
		e.preventDefault();
		sendInput({ type: "keyup", key: e.key });
	}

	// Init
	if (authToken) {
		api("GET", "/api/admin/stats")
			.then(() => showDashboard())
			.catch(() => showLogin());
	} else {
		showLogin();
	}
})();
