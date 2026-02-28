// gem2api Admin Panel
(() => {
	let authToken = localStorage.getItem("gem2api_token") || "";
	const API = "";

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

	// Init
	if (authToken) {
		api("GET", "/api/admin/stats")
			.then(() => showDashboard())
			.catch(() => showLogin());
	} else {
		showLogin();
	}
})();
