const storageSyncGet = (keys) =>
	new Promise((resolve) => chrome.storage.sync.get(keys, resolve));

const storageSyncSet = (items) =>
	new Promise((resolve) => chrome.storage.sync.set(items, resolve));

const storageLocalGet = (keys) =>
	new Promise((resolve) => chrome.storage.local.get(keys, resolve));

const sendMessage = (message) =>
	new Promise((resolve) => chrome.runtime.sendMessage(message, resolve));

const formatStatus = (lastSync) => {
	if (!lastSync) return "No sync yet.";
	const time = new Date(lastSync.time).toLocaleString();
	if (lastSync.ok) {
		return `Last sync: ${time} — Success (HTTP ${lastSync.status})`;
	}
	const errorText = lastSync.error ? ` — ${lastSync.error}` : "";
	return `Last sync: ${time} — Failed (HTTP ${lastSync.status ?? "?"})${errorText}`;
};

const loadSettings = async () => {
	const { serverUrl = "", connectionToken = "" } = await storageSyncGet([
		"serverUrl",
		"connectionToken",
	]);

	document.getElementById("serverUrl").value = serverUrl;
	document.getElementById("connectionToken").value = connectionToken;
};

const saveSettings = async () => {
	const serverUrl = document.getElementById("serverUrl").value.trim();
	const connectionToken = document
		.getElementById("connectionToken")
		.value.trim();

	await storageSyncSet({ serverUrl, connectionToken });
};

const refreshStatus = async () => {
	const { lastSync } = await storageLocalGet(["lastSync"]);
	document.getElementById("statusText").textContent = formatStatus(lastSync);
};

const attachListeners = () => {
	document.getElementById("serverUrl").addEventListener("input", saveSettings);
	document
		.getElementById("connectionToken")
		.addEventListener("input", saveSettings);

	document.getElementById("syncNow").addEventListener("click", async () => {
		const button = document.getElementById("syncNow");
		button.disabled = true;
		button.textContent = "Syncing...";

		await sendMessage("manual-sync");
		await refreshStatus();

		button.disabled = false;
		button.textContent = "Sync Now";
	});
};

document.addEventListener("DOMContentLoaded", async () => {
	await loadSettings();
	await refreshStatus();
	attachListeners();
});
