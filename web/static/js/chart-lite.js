(function () {
  "use strict";

  var CHART_SELECTOR = "[data-chart]";
  var RESIZE_DEBOUNCE_MS = 140;
  var MAX_VISIBLE_LABELS = 10;

  function isFiniteNumber(value) {
    return typeof value === "number" && isFinite(value);
  }

  function toFiniteNumber(value) {
    var numeric = Number(value);
    return isFiniteNumber(numeric) ? numeric : null;
  }

  function toText(value) {
    if (value === null || value === undefined) {
      return "";
    }
    return String(value);
  }

  function numericValues(values) {
    var result = [];
    for (var index = 0; index < values.length; index++) {
      if (isFiniteNumber(values[index])) {
        result.push(values[index]);
      }
    }
    return result;
  }

  function cssVar(name, fallback) {
    var raw = getComputedStyle(document.documentElement).getPropertyValue(name);
    var value = raw ? raw.trim() : "";
    return value || fallback;
  }

  function parseChartData(container) {
    var raw = container.getAttribute("data-chart");
    if (!raw) {
      return null;
    }

    try {
      var parsed = JSON.parse(raw);
      if (!parsed || typeof parsed !== "object") {
        return null;
      }

      var labelsSource = Array.isArray(parsed.labels) ? parsed.labels : [];
      var valuesSource = Array.isArray(parsed.values) ? parsed.values : [];
      var values = [];
      var labels = [];
      var kind = parsed.kind === "bar" ? "bar" : "line";
      var valueCount = kind === "line"
        ? Math.max(labelsSource.length, valuesSource.length)
        : valuesSource.length;

      for (var index = 0; index < valueCount; index++) {
        var numeric = toFiniteNumber(valuesSource[index]);
        var label = index < labelsSource.length ? toText(labelsSource[index]).trim() : "";

        if (kind === "line") {
          values.push(isFiniteNumber(numeric) ? numeric : null);
          labels.push(label || String(index + 1));
          continue;
        }

        if (!isFiniteNumber(numeric)) {
          continue;
        }
        values.push(numeric);
        labels.push(label || String(index + 1));
      }

      var baseline = toFiniteNumber(parsed.baseline);
      if (!isFiniteNumber(baseline) || baseline <= 0) {
        baseline = null;
      }

      var markerIndex = toFiniteNumber(parsed.markerIndex);
      if (!isFiniteNumber(markerIndex) || markerIndex < 0 || markerIndex >= labels.length) {
        markerIndex = null;
      } else {
        markerIndex = Math.round(markerIndex);
      }

      return {
        labels: labels,
        values: values,
        baseline: baseline,
        kind: kind,
        markerIndex: markerIndex,
        markerLabel: toText(parsed.markerLabel).trim()
      };
    } catch {
      return null;
    }
  }

  function renderMessage(container, text) {
    container.textContent = "";
    var content = document.createElement("div");
    content.className = "flex h-full items-center justify-center text-sm journal-muted";
    content.textContent = text;
    container.appendChild(content);
  }

  function getContainerSize(container) {
    var width = Math.max(240, Math.floor(container.clientWidth || 640));
    var height = Math.max(190, Math.floor(container.clientHeight || 280));
    return { width: width, height: height };
  }

  function createCanvas(container, size) {
    var canvas = document.createElement("canvas");
    var context = canvas.getContext("2d");
    if (!context) {
      return null;
    }
    var dpr = Math.max(1, window.devicePixelRatio || 1);

    canvas.width = Math.floor(size.width * dpr);
    canvas.height = Math.floor(size.height * dpr);
    canvas.className = "chart-canvas";
    container.appendChild(canvas);

    context.scale(dpr, dpr);

    return {
      canvas: canvas,
      context: context
    };
  }

  function createDomain(values, baseline, kind) {
    var rangeValues = numericValues(values);
    if (isFiniteNumber(baseline)) {
      rangeValues.push(baseline);
    }

    if (!rangeValues.length) {
      return null;
    }

    var minValue = kind === "bar" ? 0 : Math.min.apply(null, rangeValues);
    var maxValue = Math.max.apply(null, rangeValues);

    if (kind === "bar" && maxValue <= 0) {
      maxValue = 1;
    }

    if (minValue === maxValue) {
      minValue -= 1;
      maxValue += 1;
    }

    return {
      min: minValue,
      max: maxValue
    };
  }

  function formatChartValue(value, suffix, decimals) {
    var precision = Math.max(0, Math.min(2, Math.round(Number(decimals) || 0)));
    var numeric = Number(value);
    if (!isFiniteNumber(numeric)) {
      return "";
    }

    var rendered = precision > 0 ? numeric.toFixed(precision) : String(Math.round(numeric));
    return rendered + String(suffix || "");
  }

  function drawGrid(context, padding, width, height, color) {
    context.strokeStyle = color;
    context.lineWidth = 1;
    context.beginPath();

    for (var row = 0; row < 4; row++) {
      var y = padding.top + (height / 3) * row;
      context.moveTo(padding.left, y);
      context.lineTo(padding.left + width, y);
    }

    context.stroke();
  }

  function drawBaseline(context, padding, width, yForValue, baseline, baselineLabel, valueSuffix, valueDecimals, color) {
    var baselineY = yForValue(baseline);

    context.save();
    context.setLineDash([6, 4]);
    context.strokeStyle = color;
    context.lineWidth = 2;
    context.beginPath();
    context.moveTo(padding.left, baselineY);
    context.lineTo(padding.left + width, baselineY);
    context.stroke();
    context.restore();

    if (baselineLabel && baselineLabel.trim()) {
      context.fillStyle = color;
      context.font = "10px Quicksand, Nunito, sans-serif";
      context.textAlign = "right";
      context.textBaseline = "bottom";
      context.fillText(
        baselineLabel + " " + formatChartValue(baseline, valueSuffix, valueDecimals),
        padding.left + width - 8,
        Math.max(padding.top + 12, baselineY - 6)
      );
    }
  }

  function drawValueLine(context, values, xForIndex, yForValue, color) {
    var hasSegment = false;
    if (!values.length) {
      return;
    }

    context.strokeStyle = color;
    context.lineWidth = 3;
    context.beginPath();

    for (var index = 0; index < values.length; index++) {
      if (!isFiniteNumber(values[index])) {
        hasSegment = false;
        continue;
      }
      var x = xForIndex(index);
      var y = yForValue(values[index]);
      if (!hasSegment) {
        context.moveTo(x, y);
        hasSegment = true;
      } else {
        context.lineTo(x, y);
      }
    }

    context.stroke();
  }

  function drawValuePoints(context, values, xForIndex, yForValue, color) {
    context.fillStyle = color;

    for (var index = 0; index < values.length; index++) {
      if (!isFiniteNumber(values[index])) {
        continue;
      }
      context.beginPath();
      context.arc(xForIndex(index), yForValue(values[index]), 4.2, 0, Math.PI * 2);
      context.fill();
    }
  }

  function drawBars(context, values, getBarBox, fillColor) {
    context.fillStyle = fillColor;

    for (var index = 0; index < values.length; index++) {
      var box = getBarBox(index, values[index]);
      if (!box) {
        continue;
      }

      context.fillRect(box.x, box.y, box.width, box.height);
    }
  }

  function drawVerticalMarker(context, padding, height, xForIndex, markerIndex, label, color) {
    if (!isFiniteNumber(markerIndex)) {
      return;
    }

    var markerX = xForIndex(markerIndex);
    context.save();
    context.setLineDash([4, 4]);
    context.strokeStyle = color;
    context.lineWidth = 2;
    context.beginPath();
    context.moveTo(markerX, padding.top);
    context.lineTo(markerX, padding.top + height);
    context.stroke();
    context.restore();

    if (!label) {
      return;
    }

    context.fillStyle = color;
    context.font = "10px Quicksand, Nunito, sans-serif";
    context.textAlign = "left";
    context.textBaseline = "top";
    context.fillText(label, markerX + 6, padding.top + 4);
  }

  function drawXLabels(context, labels, xForIndex, canvasHeight, padding, color) {
    context.fillStyle = color;
    context.font = "12px Quicksand, Nunito, sans-serif";
    context.textAlign = "center";
    context.textBaseline = "top";

    if (!labels.length) {
      return;
    }

    var step = Math.max(1, Math.ceil(labels.length / MAX_VISIBLE_LABELS));
    var lastDrawnIndex = -1;
    for (var index = 0; index < labels.length; index += step) {
      context.fillText(labels[index], xForIndex(index), canvasHeight - padding.bottom + 10);
      lastDrawnIndex = index;
    }

    var lastIndex = labels.length - 1;
    if (lastDrawnIndex !== lastIndex) {
      context.fillText(labels[lastIndex], xForIndex(lastIndex), canvasHeight - padding.bottom + 10);
    }
  }

  function drawYLabels(context, domain, padding, height, valueSuffix, valueDecimals, color) {
    context.fillStyle = color;
    context.font = "12px Quicksand, Nunito, sans-serif";
    context.textAlign = "right";
    context.textBaseline = "middle";
    context.fillText(formatChartValue(domain.max, valueSuffix, valueDecimals), padding.left - 8, padding.top + 2);
    context.fillText(formatChartValue(domain.min, valueSuffix, valueDecimals), padding.left - 8, padding.top + height);
  }

  function drawChart(container) {
    if (!container) {
      return;
    }

    var emptyText = container.getAttribute("data-empty-text") || "Not enough cycle data yet.";
    var valueSuffix = container.getAttribute("data-value-suffix");
    if (valueSuffix === null) {
      valueSuffix = container.getAttribute("data-days-suffix") || "d";
    }
    var valueDecimals = Number(container.getAttribute("data-value-decimals"));
    if (!isFiniteNumber(valueDecimals)) {
      valueDecimals = valueSuffix === "d" ? 0 : 1;
    }
    var baselineLabel = container.getAttribute("data-baseline-label");
    if (baselineLabel === null) {
      baselineLabel = "Baseline";
    }
    var chartData = parseChartData(container);

    container.textContent = "";

    if (!chartData) {
      renderMessage(container, "Unable to render chart.");
      return;
    }

    var hasBaseline = isFiniteNumber(chartData.baseline);
    if (!numericValues(chartData.values).length && !hasBaseline) {
      renderMessage(container, emptyText);
      return;
    }

    var size = getContainerSize(container);
    var canvasBundle = createCanvas(container, size);
    if (!canvasBundle) {
      renderMessage(container, "Unable to render chart.");
      return;
    }
    var context = canvasBundle.context;
    var padding = { top: 26, right: 22, bottom: 40, left: 46 };
    var innerWidth = size.width - padding.left - padding.right;
    var innerHeight = size.height - padding.top - padding.bottom;
    var domain = createDomain(chartData.values, chartData.baseline, chartData.kind);

    if (!domain) {
      renderMessage(container, emptyText);
      return;
    }

    var xForIndex = function (index) {
      if (chartData.labels.length <= 1) {
        return padding.left + innerWidth / 2;
      }
      if (chartData.kind === "bar") {
        return padding.left + ((index + 0.5) * innerWidth) / chartData.labels.length;
      }
      return padding.left + (index * innerWidth) / (chartData.labels.length - 1);
    };

    var yForValue = function (value) {
      var ratio = (value - domain.min) / (domain.max - domain.min);
      return padding.top + innerHeight - ratio * innerHeight;
    };

    var barBaseY = yForValue(Math.max(domain.min, 0));
    var barSlotWidth = chartData.labels.length > 0 ? innerWidth / chartData.labels.length : innerWidth;
    var barWidth = Math.min(36, Math.max(16, barSlotWidth * 0.62));
    var getBarBox = function (index, value) {
      if (!isFiniteNumber(value)) {
        return null;
      }
      var centerX = xForIndex(index);
      var topY = yForValue(value);
      var height = Math.max(4, barBaseY - topY);

      return {
        x: centerX - barWidth / 2,
        y: barBaseY - height,
        width: barWidth,
        height: height
      };
    };

    var colors = {
      grid: cssVar("--chart-grid", "rgba(172, 136, 96, 0.26)"),
      line: cssVar("--chart-line", "#c4895a"),
      dot: cssVar("--chart-dot", "#b9753e"),
      baseline: cssVar("--chart-baseline", "#9f8a75"),
      label: cssVar("--text-muted", "#9b8b7a")
    };

    context.clearRect(0, 0, size.width, size.height);
    drawGrid(context, padding, innerWidth, innerHeight, colors.grid);

    if (hasBaseline) {
      drawBaseline(context, padding, innerWidth, yForValue, chartData.baseline, baselineLabel, valueSuffix, valueDecimals, colors.baseline);
    }

    if (chartData.kind === "bar") {
      drawBars(context, chartData.values, getBarBox, colors.dot);
    } else {
      drawValueLine(context, chartData.values, xForIndex, yForValue, colors.line);
      drawValuePoints(context, chartData.values, xForIndex, yForValue, colors.dot);
    }
    if (isFiniteNumber(chartData.markerIndex)) {
      drawVerticalMarker(context, padding, innerHeight, xForIndex, chartData.markerIndex, chartData.markerLabel, colors.baseline);
    }
    drawXLabels(context, chartData.labels, xForIndex, size.height, padding, colors.label);
    drawYLabels(context, domain, padding, innerHeight, valueSuffix, valueDecimals, colors.label);
  }

  function renderCharts(root) {
    var scope = root && root.querySelectorAll ? root : document;
    if (scope !== document && scope.matches && scope.matches(CHART_SELECTOR)) {
      drawChart(scope);
    }

    var charts = scope.querySelectorAll(CHART_SELECTOR);
    for (var index = 0; index < charts.length; index++) {
      drawChart(charts[index]);
    }
  }

  var resizeTimer = null;
  function scheduleRender() {
    if (resizeTimer !== null) {
      clearTimeout(resizeTimer);
    }
    resizeTimer = setTimeout(function () {
      renderCharts(document);
    }, RESIZE_DEBOUNCE_MS);
  }

  window.addEventListener("DOMContentLoaded", function () {
    renderCharts(document);
  });

  window.addEventListener("resize", scheduleRender);

  document.body.addEventListener("htmx:afterSwap", function (event) {
    var target = event && event.detail ? event.detail.target : null;
    renderCharts(target || document);
  });
})();
