require("dotenv").config();
const API_BASE = process.env.API_BASE;
let serverOnline = false;

// Check server status on page load
window.onload = function () {
  testHealth(true);

  // File input change handler
  document.getElementById("fileInput").addEventListener("change", function (e) {
    const files = e.target.files;
    const uploadBtn = document.getElementById("upload-btn");
    const fileInfo = document.getElementById("file-info");

    if (files.length > 0) {
      let totalSize = 0;
      let fileTypes = {};

      for (let file of files) {
        totalSize += file.size;
        const ext = file.name.split(".").pop().toLowerCase();
        fileTypes[ext] = (fileTypes[ext] || 0) + 1;
      }

      const sizeInMB = (totalSize / (1024 * 1024)).toFixed(2);
      const typesList = Object.entries(fileTypes)
        .map(([ext, count]) => `${ext} (${count})`)
        .join(", ");

      fileInfo.innerHTML = `
                        <strong>Selected:</strong> ${files.length} files<br>
                        <strong>Total Size:</strong> ${sizeInMB} MB<br>
                        <strong>File Types:</strong> ${typesList}<br>
                        <strong>Root Directory:</strong> ${
                          files[0].webkitRelativePath.split("/")[0]
                        }
                    `;
      fileInfo.style.display = "block";
      uploadBtn.disabled = false;
    } else {
      fileInfo.style.display = "none";
      uploadBtn.disabled = true;
    }
  });
};

async function testHealth(silent = false) {
  const btn = document.getElementById("health-btn");
  const responseDiv = document.getElementById("health-response");
  const statusDiv = document.getElementById("server-status");

  if (!silent) {
    btn.disabled = true;
    btn.textContent = "Checking...";
  }

  try {
    const response = await fetch(`${API_BASE}/health`);
    const data = await response.json();

    if (!silent) {
      responseDiv.innerHTML = `<span class="success">✅ Server is healthy!<br>Response: ${JSON.stringify(
        data,
        null,
        2
      )}</span>`;
      responseDiv.style.display = "block";
    }

    statusDiv.className = "status online";
    statusDiv.textContent = "Server Online";
    serverOnline = true;
  } catch (error) {
    if (!silent) {
      responseDiv.innerHTML = `<span class="error">❌ Failed to connect to server<br>Error: ${error.message}<br><br>Make sure the Go server is running on port 8080.</span>`;
      responseDiv.style.display = "block";
    }

    statusDiv.className = "status offline";
    statusDiv.textContent = "Server Offline";
    serverOnline = false;
  } finally {
    if (!silent) {
      btn.disabled = false;
      btn.textContent = "Check Server Health";
    }
  }
}

async function uploadFiles() {
  const fileInput = document.getElementById("fileInput");
  const files = fileInput.files;
  const btn = document.getElementById("upload-btn");
  const responseDiv = document.getElementById("upload-response");
  const progressDiv = document.getElementById("upload-progress");
  const progressBar = document.getElementById("progress-bar");

  if (files.length === 0) {
    alert("Please select a directory first");
    return;
  }

  btn.disabled = true;
  btn.textContent = "Uploading...";
  progressDiv.style.display = "block";
  progressBar.style.width = "0%";

  const formData = new FormData();
  let processedFiles = 0;

  for (let file of files) {
    formData.append("files", file);
    formData.append(`path_${file.name}`, file.webkitRelativePath);
    processedFiles++;

    // Update progress
    const progress = (processedFiles / files.length) * 50; // 50% for file processing
    progressBar.style.width = progress + "%";
  }

  try {
    const response = await fetch(`${API_BASE}/upload`, {
      method: "POST",
      body: formData,
    });

    progressBar.style.width = "100%";

    const data = await response.json();
    const responseClass = data.success ? "success" : "error";
    const icon = data.success ? "✅" : "❌";

    responseDiv.innerHTML = `<span class="${responseClass}">${icon} Upload ${
      data.success ? "Successful" : "Failed"
    }!<br><pre>${JSON.stringify(data, null, 2)}</pre></span>`;
    responseDiv.style.display = "block";

    // Auto-fill directory ID for testing
    if (data.directory_id) {
      document.getElementById("directoryId").value = data.directory_id;
    }
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Upload failed<br>Error: ${error.message}</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Upload Selected Directory";
    setTimeout(() => {
      progressDiv.style.display = "none";
    }, 2000);
  }
}

async function listCodebases() {
  const btn = document.getElementById("list-btn");
  const responseDiv = document.getElementById("list-response");

  btn.disabled = true;
  btn.textContent = "Loading...";

  try {
    const response = await fetch(`${API_BASE}/codebases`);
    const data = await response.json();

    responseDiv.innerHTML = `<span class="success">✅ Codebases Retrieved<br><pre>${JSON.stringify(
      data,
      null,
      2
    )}</pre></span>`;
    responseDiv.style.display = "block";
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Failed to retrieve codebases<br>Error: ${error.message}</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "List All Codebases";
  }
}

async function getCodebaseDetails() {
  const directoryId = document.getElementById("directoryId").value.trim();
  const btn = document.getElementById("details-btn");
  const responseDiv = document.getElementById("details-response");

  if (!directoryId) {
    alert("Please enter a directory UUID");
    return;
  }

  btn.disabled = true;
  btn.textContent = "Loading...";

  try {
    const response = await fetch(`${API_BASE}/codebases/${directoryId}`);
    const data = await response.json();

    const responseClass = data.success ? "success" : "error";
    const icon = data.success ? "✅" : "❌";

    responseDiv.innerHTML = `<span class="${responseClass}">${icon} Codebase Details<br><pre>${JSON.stringify(
      data,
      null,
      2
    )}</pre></span>`;
    responseDiv.style.display = "block";
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Failed to retrieve codebase details<br>Error: ${error.message}</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Get Codebase Details";
  }
}
