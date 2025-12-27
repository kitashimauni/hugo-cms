async function runSync() {
    if(!confirm("GitHubから最新の状態を取得しますか？\n（ローカルの未保存の変更は注意してください）")) return;
    
    const btn = document.querySelector('button[onclick="runSync()"]');
    const originalText = btn.textContent;
    btn.textContent = "Syncing...";
    
    try {
        const res = await fetch('/api/sync', { method: 'POST' });
        const data = await res.json();
        if(data.status === 'ok') {
            alert("Sync Complete!\n" + data.log);
            fetchFiles(); // リスト更新
        } else {
            alert("Sync Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error");
    } finally {
        btn.textContent = originalText;
    }
}

async function runPublish() {
    if(!confirm("この記事の変更をGitHubにPushして公開しますか？")) return;

    const btn = document.querySelector('button[onclick="runPublish()"]');
    btn.textContent = "Pushing...";
    btn.disabled = true;

    try {
        const res = await fetch('/api/publish', { method: 'POST' });
        const data = await res.json();
        if(data.status === 'ok') {
            alert("Published Successfully! 🚀\nCloudflare Pages will deploy shortly.");
        } else {
            alert("Publish Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error");
    } finally {
        btn.textContent = "Publish";
        btn.disabled = false;
    }
}
