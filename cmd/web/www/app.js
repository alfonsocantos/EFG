const { createApp, ref, onMounted, onBeforeUnmount } = Vue;

createApp({
  setup() {
    const playerID = ref('demo-alex');
    const friends = ref([]);
    const message = ref('');
    const error = ref('');
    const health = ref(null);
    const healthError = ref('');
    let healthTimer;
    let healthRequest;
    const activePlayer = ref('');
    const myStatus = ref(null);
    const savingStatus = ref(false);
    const statusError = ref('');
    let saveRequest;
    let timer;
    let request;

    async function refresh() {
      const controller = new AbortController();
      request = controller;
      const timeout = setTimeout(() => controller.abort(), 5000);
      try {
        const options = {
          headers: { 'X-Player-ID': activePlayer.value },
          cache: 'no-store',
          signal: controller.signal,
        };
        const [friendsResponse, statusResponse] = await Promise.all([
          fetch('/api/me/friends/presence', options),
          fetch('/api/me/status', options),
        ]);
        if (!friendsResponse.ok || !statusResponse.ok) throw new Error('Unable to load presence.');
        const [list, status] = await Promise.all([friendsResponse.json(), statusResponse.json()]);
        if (request !== controller) return;
        friends.value = list;
        myStatus.value = status;
        error.value = '';
        message.value = list.length ? `${list.length} friends for ${activePlayer.value}.` : 'No friends found for this player.';
      } catch (failure) {
        if (request !== controller) return;
        myStatus.value = null;
        error.value = 'Connection failed. Displayed presence may be out of date. Retrying…';
      } finally {
        clearTimeout(timeout);
        // Schedule after completion so slow requests never overlap.
        if (request === controller) timer = setTimeout(refresh, 1000);
      }
    }

    async function refreshHealth() {
      const controller = new AbortController();
      healthRequest = controller;
      const timeout = setTimeout(() => controller.abort(), 5000);
      try {
        const response = await fetch('/api/health', { cache: 'no-store', signal: controller.signal });
        if (!response.ok) throw new Error('Health unavailable');
        const snapshot = await response.json();
        if (healthRequest !== controller) return;
        health.value = snapshot;
        healthError.value = '';
      } catch {
        if (healthRequest !== controller) return;
        health.value = null;
        healthError.value = 'Server health unavailable. Retrying…';
      } finally {
        clearTimeout(timeout);
        if (healthRequest === controller) healthTimer = setTimeout(refreshHealth, 1000);
      }
    }

    function stop() {
      clearTimeout(timer);
      request?.abort();
      request = null;
    }

    function showFriends() {
      if (savingStatus.value) return;
      stop();
      activePlayer.value = playerID.value.trim();
      myStatus.value = null;
      statusError.value = '';
      friends.value = [];
      error.value = '';
      message.value = activePlayer.value ? 'Loading…' : 'Enter a player ID.';
      if (activePlayer.value) refresh();
    }

    async function toggleOfflineMode() {
      if (!myStatus.value || savingStatus.value) return;
      stop(); // Prevent an older poll from overwriting the saved preference.
      savingStatus.value = true;
      statusError.value = '';
      const controller = new AbortController();
      saveRequest = controller;
      const timeout = setTimeout(() => controller.abort(), 5000);
      try {
        const response = await fetch('/api/me/status', {
          method: 'PUT',
          headers: { 'X-Player-ID': activePlayer.value, 'Content-Type': 'application/json' },
          body: JSON.stringify({ offline_mode: !myStatus.value.offline_mode }),
          signal: controller.signal,
        });
        if (!response.ok) throw new Error('Unable to save visibility.');
      } catch {
        statusError.value = 'Could not confirm the visibility change. Check your status and try again.';
      } finally {
        clearTimeout(timeout);
        if (saveRequest === controller) {
          saveRequest = null;
          savingStatus.value = false;
          myStatus.value = null;
          refresh();
        }
      }
    }

    function stateClass(state) {
      if (state === 'online') return 'bg-emerald-100 text-emerald-800';
      if (state === 'ingame') return 'bg-blue-100 text-blue-800';
      return 'bg-slate-100 text-slate-600';
    }

    onMounted(() => { showFriends(); refreshHealth(); });
    onBeforeUnmount(() => {
      stop();
      saveRequest?.abort();
      saveRequest = null;
      clearTimeout(healthTimer);
      healthRequest?.abort();
      healthRequest = null;
    });
    return { playerID, activePlayer, myStatus, savingStatus, statusError, toggleOfflineMode, friends, message, error, health, healthError, showFriends, stateClass };
  },
}).mount('#app');
