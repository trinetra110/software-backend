const API_BASE = "http://localhost:8080";
let serverOnline = false;

// Check server status on page load
window.onload = function () {
  testHealth(true);

  // Upload type change handler
  document.querySelectorAll('input[name="uploadType"]').forEach((radio) => {
    radio.addEventListener("change", function () {
      const fileInput = document.getElementById("fileInput");
      if (this.value === "directory") {
        fileInput.setAttribute("webkitdirectory", "");
        fileInput.setAttribute("directory", "");
      } else {
        fileInput.removeAttribute("webkitdirectory");
        fileInput.removeAttribute("directory");
      }
      fileInput.value = ""; // Clear current selection
      document.getElementById("file-info").style.display = "none";
      document.getElementById("upload-btn").disabled = true;
    });
  });

  // File input change handler
  document.getElementById("fileInput").addEventListener("change", function (e) {
    const files = e.target.files;
    const uploadBtn = document.getElementById("upload-btn");
    const fileInfo = document.getElementById("file-info");
    const uploadType = document.querySelector(
      'input[name="uploadType"]:checked'
    ).value;

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

      let rootDir = "";
      if (uploadType === "directory" && files[0].webkitRelativePath) {
        rootDir = files[0].webkitRelativePath.split("/")[0];
      }

      fileInfo.innerHTML = `
                        <strong>Selected:</strong> ${
                          files.length
                        } files (${uploadType})<br>
                        <strong>Total Size:</strong> ${sizeInMB} MB<br>
                        <strong>File Types:</strong> ${typesList}${
        rootDir ? `<br><strong>Root Directory:</strong> ${rootDir}` : ""
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
      responseDiv.innerHTML = `<span class="error">❌ Failed to connect to server<br>Error: ${error.message}<br><br>Make sure the Go server is running on port 8080 and has the missing functions added.</span>`;
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
  const uploadType = document.querySelector(
    'input[name="uploadType"]:checked'
  ).value;

  if (files.length === 0) {
    alert("Please select files first");
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

    // Handle path based on upload type
    if (uploadType === "directory" && file.webkitRelativePath) {
      // For directory uploads, use the relative path
      formData.append(`path_${file.name}`, file.webkitRelativePath);
    } else {
      // For individual files, just use the filename
      formData.append(`path_${file.name}`, file.name);
    }

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

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json();
    const responseClass = data.success ? "success" : "error";
    const icon = data.success ? "✅" : "❌";

    responseDiv.innerHTML = `<span class="${responseClass}">${icon} Upload ${
      data.success ? "Successful" : "Failed"
    }!<br><pre>${JSON.stringify(data, null, 2)}</pre></span>`;
    responseDiv.style.display = "block";

    // Auto-fill directory ID fields for testing
    if (data.directory_id) {
      document.getElementById("directoryId").value = data.directory_id;
      document.getElementById("readDirectoryId").value = data.directory_id;
      document.getElementById("downloadDirectoryId").value = data.directory_id;
      document.getElementById("zipDirectoryId").value = data.directory_id;
    }
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Upload failed<br>Error: ${error.message}<br><br>This might be due to missing respondWithError() function in Go server.</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Upload Selected Files";
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

async function readFileMetadata() {
  const directoryId = document.getElementById("readDirectoryId").value.trim();
  const filePath = document.getElementById("readFilePath").value.trim();
  const btn = document.getElementById("read-btn");
  const responseDiv = document.getElementById("read-response");

  if (!directoryId || !filePath) {
    alert("Please enter both directory UUID and file path");
    return;
  }

  btn.disabled = true;
  btn.textContent = "Reading...";

  try {
    const response = await fetch(
      `${API_BASE}/codebases/${directoryId}/content?file=${encodeURIComponent(
        filePath
      )}`
    );

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json();
    const responseClass = data.success ? "success" : "error";
    const icon = data.success ? "✅" : "❌";

    responseDiv.innerHTML = `<span class="${responseClass}">${icon} File Metadata<br><pre>${JSON.stringify(
      data,
      null,
      2
    )}</pre></span>`;
    responseDiv.style.display = "block";
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Failed to read file metadata<br>Error: ${error.message}<br><br>This route might be missing from your Go server's main() function.</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Read File Content";
  }
}

async function downloadFile() {
  const directoryId = document
    .getElementById("downloadDirectoryId")
    .value.trim();
  const filePath = document.getElementById("downloadFilePath").value.trim();
  const btn = document.getElementById("download-btn");
  const responseDiv = document.getElementById("download-response");

  if (!directoryId || !filePath) {
    alert("Please enter both directory UUID and file path");
    return;
  }

  btn.disabled = true;
  btn.textContent = "Downloading...";

  try {
    const response = await fetch(
      `${API_BASE}/codebases/${directoryId}/download?file=${encodeURIComponent(
        filePath
      )}`
    );

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    // Create download link
    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filePath.split("/").pop(); // Get filename from path
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);

    responseDiv.innerHTML = `<span class="success">✅ File downloaded successfully!</span>`;
    responseDiv.style.display = "block";
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Failed to download file<br>Error: ${error.message}<br><br>This route might be missing from your Go server's main() function.</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Download File";
  }
}

async function downloadZip() {
  const directoryId = document.getElementById("zipDirectoryId").value.trim();
  const btn = document.getElementById("zip-btn");
  const responseDiv = document.getElementById("zip-response");

  if (!directoryId) {
    alert("Please enter a directory UUID");
    return;
  }

  btn.disabled = true;
  btn.textContent = "Downloading ZIP...";

  try {
    const response = await fetch(`${API_BASE}/codebases/${directoryId}/zip`);

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    // Create download link
    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `codebase-${directoryId}.zip`;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);

    responseDiv.innerHTML = `<span class="success">✅ ZIP file downloaded successfully!</span>`;
    responseDiv.style.display = "block";
  } catch (error) {
    responseDiv.innerHTML = `<span class="error">❌ Failed to download ZIP<br>Error: ${error.message}<br><br>This route might be missing from your Go server's main() function.</span>`;
    responseDiv.style.display = "block";
  } finally {
    btn.disabled = false;
    btn.textContent = "Download as ZIP";
  }
}