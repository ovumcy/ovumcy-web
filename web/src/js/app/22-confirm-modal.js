  function initConfirmModal() {
    var modal = document.getElementById("confirm-modal");
    var messageNode = document.getElementById("confirm-modal-message");
    var cancelButton = document.getElementById("confirm-modal-cancel");
    var acceptButton = document.getElementById("confirm-modal-accept");
    if (!modal || !messageNode || !cancelButton || !acceptButton) {
      return;
    }

    var pendingResolve = null;
    var previouslyFocused = null;

    function closeConfirm(accepted) {
      if (!pendingResolve) {
        return;
      }
      var resolve = pendingResolve;
      pendingResolve = null;
      modal.classList.add("hidden");
      modal.setAttribute("aria-hidden", "true");
      if (previouslyFocused && typeof previouslyFocused.focus === "function" && document.contains(previouslyFocused)) {
        previouslyFocused.focus();
      }
      previouslyFocused = null;
      resolve(accepted);
    }

    function openConfirm(question, acceptLabel) {
      if (pendingResolve) {
        pendingResolve(false);
        pendingResolve = null;
      }

      previouslyFocused = document.activeElement && document.activeElement !== document.body
        ? document.activeElement
        : null;

      messageNode.textContent = question || "";
      cancelButton.textContent = document.body.getAttribute("data-confirm-cancel") || "Cancel";
      acceptButton.textContent = acceptLabel || document.body.getAttribute("data-confirm-delete") || "Delete";
      modal.classList.remove("hidden");
      modal.setAttribute("aria-hidden", "false");
      cancelButton.focus();

      return new Promise(function (resolve) {
        pendingResolve = resolve;
      });
    }

    window.__ovumcyOpenConfirm = openConfirm;

    cancelButton.addEventListener("click", function () {
      closeConfirm(false);
    });

    acceptButton.addEventListener("click", function () {
      closeConfirm(true);
    });

    modal.addEventListener("click", function (event) {
      if (event.target === modal) {
        closeConfirm(false);
      }
    });

    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        closeConfirm(false);
        return;
      }

      if (event.key !== "Tab" || !pendingResolve) {
        return;
      }

      // The dialog exposes exactly two focusable controls, so the trap
      // cycles between them; anything outside the modal re-enters at the
      // edge matching the tab direction.
      var first = cancelButton;
      var last = acceptButton;
      var active = document.activeElement;
      if (event.shiftKey) {
        if (active === first || !modal.contains(active)) {
          event.preventDefault();
          last.focus();
        }
        return;
      }
      if (active === last || !modal.contains(active)) {
        event.preventDefault();
        first.focus();
      }
    });

    document.body.addEventListener("htmx:confirm", function (event) {
      if (!event || !event.detail || !event.detail.question) {
        return;
      }

      var source = event.detail.elt || event.target;
      if (!source || !source.getAttribute) {
        return;
      }

      var acceptLabel = source.getAttribute("data-confirm-accept") || "";
      event.preventDefault();
      openConfirm(event.detail.question, acceptLabel).then(function (confirmed) {
        if (confirmed) {
          event.detail.issueRequest(true);
        }
      });
    });

    document.addEventListener("submit", function (event) {
      var form = event.target;
      if (!form || !form.matches || !form.matches("form[data-confirm]")) {
        return;
      }

      if (form.dataset.confirmBypass === "1") {
        form.dataset.confirmBypass = "";
        return;
      }

      event.preventDefault();
      openConfirm(form.getAttribute("data-confirm") || "", form.getAttribute("data-confirm-accept") || "").then(function (confirmed) {
        if (!confirmed) {
          return;
        }
        form.dataset.confirmBypass = "1";
        if (typeof form.requestSubmit === "function") {
          form.requestSubmit();
          return;
        }
        form.submit();
      });
    });
  }
