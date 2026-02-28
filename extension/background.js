const ALARM_NAME = "sync-cookies";
const DEFAULT_PERIOD_MINUTES = 30;

const storageSyncGet = (keys) =>
	new Promise((resolve) => chrome.storage.sync.get(keys, resolve));

const storageLocalSet = (items) =>
	new Promise((resolve) => chrome.storage.local.set(items, resolve));

const cookiesGet = (details) =>
	new Promise((resolve) => chrome.cookies.get(details, resolve));

const alarmsCreate = (name, info) =>
	new Promise((resolve) => chrome.alarms.create(name, info) || resolve());

const normalizeServerUrl = (serverUrl) => {
	const url = new URL(serverUrl);
	return new URL("/api/cookies/update", url).toString();
};

const readCookieValue = async (name) => {
	const cookie = await cookiesGet({
		url: "https://google.com",
		name,
	});
	return cookie?.value ?? "";
};

const syncCookies = async () => {
	const { serverUrl, connectionToken } = await storageSyncGet([
		"serverUrl",
		"connectionToken",
	]);

	const lastSync = {
		time: new Date().toISOString(),
		ok: false,
		status: null,
		error: null,
	};

	try {
		if (!serverUrl || !connectionToken) {
			throw new Error("Missing server URL or connection token.");
		}

		const endpoint = normalizeServerUrl(serverUrl);

		const [secure1psid, secure1psidts] = await Promise.all([
			readCookieValue("__Secure-1PSID"),
			readCookieValue("__Secure-1PSIDTS"),
		]);

		const response = await fetch(endpoint, {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Authorization: `Bearer ${connectionToken}`,
			},
			body: JSON.stringify({
				secure_1psid: secure1psid,
				secure_1psidts: secure1psidts,
			}),
		});

		lastSync.ok = response.ok;
		lastSync.status = response.status;
		if (!response.ok) {
			lastSync.error = `Request failed with status ${response.status}.`;
		}
	} catch (error) {
		lastSync.error = error instanceof Error ? error.message : String(error);
	}

	await storageLocalSet({ lastSync });
};

chrome.runtime.onInstalled.addListener(async () => {
	await alarmsCreate(ALARM_NAME, {
		periodInMinutes: DEFAULT_PERIOD_MINUTES,
	});
	await syncCookies();
});

chrome.alarms.onAlarm.addListener((alarm) => {
	if (alarm.name === ALARM_NAME) {
		void syncCookies();
	}
});

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
	if (message === "manual-sync") {
		syncCookies()
			.then(() => sendResponse({ ok: true }))
			.catch((error) =>
				sendResponse({
					ok: false,
					error: error instanceof Error ? error.message : String(error),
				}),
			);
		return true;
	}
	return false;
});
