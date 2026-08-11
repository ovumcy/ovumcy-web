(function () {
  "use strict";

  var CHART_SELECTOR = "[data-chart]";
  var RESIZE_DEBOUNCE_MS = 140;
  var MAX_VISIBLE_LABELS = 10;

  // The "line" series is the current cycle's basal body temperature in °C,
  // whose whole subject is a ~0.3 °C biphasic shift: on a zero-based or
  // otherwise data-agnostic axis that shift is invisible. The domain is the
  // reading range plus a little air, widened to a floor window so that a flat
  // cycle is not magnified into noise instead.
  var BBT_DOMAIN_PADDING = 0.15;
  var BBT_DOMAIN_MIN_SPAN = 0.8;
  var BBT_TICK_STEP = 0.2;

  // The "bar" series is one cycle length in whole days per completed cycle.
  var CYCLE_DOMAIN_PADDING = 1;
  var CYCLE_DOMAIN_MIN_SPAN = 4;

  var MAX_TICKS = 8;
  var SERIES_LINE_WIDTH = 2;
  var HAIRLINE_WIDTH = 1;
  var MARKER_RADIUS = 4.5;
  var MARKER_RING_WIDTH = 2;
  var BASELINE_LABEL_INSET = 12;

  function isFiniteNumber(value) {
    return typeof value === "number" && isFinite(value);
  }

  // A non-value is a gap, never a reading: the server emits JSON null for a
  // day with no measurement, and Number(null) is 0 — a value that passes every
  // finite check, draws an unlogged day as a real 0 °C point and drags the
  // axis down to zero. Reject the non-values before Number() ever sees them.
  function toFiniteNumber(value) {
    if (value === null || value === undefined) {
      return null;
    }
    if (typeof value === "string" && value.trim() === "") {
      return null;
    }
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
    canvas.setAttribute("aria-hidden", "true");
    canvas.style.display = "block";
    canvas.style.width = size.width + "px";
    canvas.style.height = size.height + "px";
    canvas.style.maxWidth = "100%";
    container.appendChild(canvas);

    context.scale(dpr, dpr);

    return {
      canvas: canvas,
      context: context
    };
  }

  function roundValue(value) {
    return Math.round(value * 1e6) / 1e6;
  }

  // widenToSpan grows a range around its centre until it is at least minSpan
  // wide, so a cycle whose readings barely move is not blown up to full height.
  function widenToSpan(min, max, minSpan) {
    var span = max - min;
    if (span >= minSpan) {
      return { min: min, max: max };
    }
    var grow = (minSpan - span) / 2;
    return { min: min - grow, max: max + grow };
  }

  function fitTickStep(span, step) {
    var fitted = step > 0 ? step : 1;
    while (span / fitted > MAX_TICKS) {
      fitted *= 2;
    }
    return fitted;
  }

  function createDomain(values, baseline, kind) {
    var rangeValues = numericValues(values);
    if (isFiniteNumber(baseline)) {
      rangeValues.push(baseline);
    }

    if (!rangeValues.length) {
      return null;
    }

    var minValue = Math.min.apply(null, rangeValues);
    var maxValue = Math.max.apply(null, rangeValues);

    if (kind === "bar") {
      var days = widenToSpan(
        Math.floor(minValue) - CYCLE_DOMAIN_PADDING,
        Math.ceil(maxValue) + CYCLE_DOMAIN_PADDING,
        CYCLE_DOMAIN_MIN_SPAN
      );
      var dayMin = Math.floor(days.min);
      var dayMax = Math.ceil(days.max);
      return {
        min: dayMin,
        max: dayMax,
        step: fitTickStep(dayMax - dayMin, Math.max(1, Math.ceil((dayMax - dayMin) / 4)))
      };
    }

    var reading = widenToSpan(
      minValue - BBT_DOMAIN_PADDING,
      maxValue + BBT_DOMAIN_PADDING,
      BBT_DOMAIN_MIN_SPAN
    );
    return {
      min: roundValue(reading.min),
      max: roundValue(reading.max),
      step: fitTickStep(reading.max - reading.min, BBT_TICK_STEP)
    };
  }

  // domainTicks are the multiples of the domain's step that fall inside it —
  // the values that get a grid hairline and an axis label.
  function domainTicks(domain) {
    var step = isFiniteNumber(domain.step) && domain.step > 0 ? domain.step : 0;
    if (!step) {
      return [domain.min, domain.max];
    }

    var ticks = [];
    var value = Math.ceil(roundValue(domain.min / step)) * step;
    for (var guard = 0; guard <= MAX_TICKS + 1; guard++) {
      var tick = roundValue(value);
      if (tick > domain.max + step / 1000) {
        break;
      }
      ticks.push(tick);
      value += step;
    }

    return ticks.length ? ticks : [domain.min, domain.max];
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

  // The grid is a set of solid hairlines, one per axis tick: a dashed grid
  // competes with the dashed reference line for meaning.
  function drawGrid(context, padding, width, ticks, yForValue, color) {
    context.save();
    context.setLineDash([]);
    context.strokeStyle = color;
    context.lineWidth = HAIRLINE_WIDTH;
    context.beginPath();

    for (var index = 0; index < ticks.length; index++) {
      var y = yForValue(ticks[index]);
      context.moveTo(padding.left, y);
      context.lineTo(padding.left + width, y);
    }

    context.stroke();
    context.restore();
  }

  function drawBaseline(context, padding, width, height, yForValue, baseline, baselineLabel, valueSuffix, valueDecimals, color) {
    var baselineY = yForValue(baseline);

    context.save();
    context.setLineDash([6, 4]);
    context.strokeStyle = color;
    context.lineWidth = SERIES_LINE_WIDTH;
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
      // The label belongs to the plot area, so it is inset from every edge of
      // it rather than pinned against the right-hand one.
      var text = baselineLabel + " " + formatChartValue(baseline, valueSuffix, valueDecimals);
      var textWidth = context.measureText ? context.measureText(text).width : 0;
      var labelRight = padding.left + width - BASELINE_LABEL_INSET;
      if (labelRight - textWidth < padding.left) {
        labelRight = padding.left + textWidth;
      }
      var labelY = Math.min(
        padding.top + height - BASELINE_LABEL_INSET / 2,
        Math.max(padding.top + BASELINE_LABEL_INSET, baselineY - 6)
      );
      context.fillText(text, labelRight, labelY);
    }
  }

  function drawValueLine(context, values, xForIndex, yForValue, color, lineWidth) {
    var hasSegment = false;
    if (!values.length) {
      return;
    }

    context.save();
    context.setLineDash([]);
    context.strokeStyle = color;
    context.lineWidth = lineWidth;
    context.lineCap = "round";
    context.lineJoin = "round";
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
    context.restore();
  }

  // A marker is a disc with a ring of the surface colour around it, so that a
  // point stays readable where the series line passes underneath it. Only a
  // logged value gets one: an unlogged day is a gap in the series.
  function drawValuePoints(context, values, xForIndex, yForValue, color, ringColor) {
    for (var index = 0; index < values.length; index++) {
      if (!isFiniteNumber(values[index])) {
        continue;
      }

      var x = xForIndex(index);
      var y = yForValue(values[index]);

      context.beginPath();
      context.arc(x, y, MARKER_RADIUS, 0, Math.PI * 2);
      context.fillStyle = color;
      context.fill();

      context.save();
      context.setLineDash([]);
      context.beginPath();
      context.arc(x, y, MARKER_RADIUS + MARKER_RING_WIDTH / 2, 0, Math.PI * 2);
      context.strokeStyle = ringColor;
      context.lineWidth = MARKER_RING_WIDTH;
      context.stroke();
      context.restore();
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

  function drawYLabels(context, ticks, padding, yForValue, valueSuffix, valueDecimals, color) {
    context.fillStyle = color;
    context.font = "12px Quicksand, Nunito, sans-serif";
    context.textAlign = "right";
    context.textBaseline = "middle";

    for (var index = ticks.length - 1; index >= 0; index--) {
      context.fillText(
        formatChartValue(ticks[index], valueSuffix, valueDecimals),
        padding.left - 8,
        yForValue(ticks[index])
      );
    }
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

    var colors = {
      grid: cssVar("--chart-grid", "rgba(172, 136, 96, 0.26)"),
      line: cssVar("--chart-line", "#a8622c"),
      dot: cssVar("--chart-dot", "#b9753e"),
      baseline: cssVar("--chart-baseline", "#9f8a75"),
      label: cssVar("--text-muted", "#9b8b7a"),
      surface: cssVar("--bg-card", "#ffffff")
    };

    var ticks = domainTicks(domain);

    context.clearRect(0, 0, size.width, size.height);
    drawGrid(context, padding, innerWidth, ticks, yForValue, colors.grid);

    if (hasBaseline) {
      drawBaseline(context, padding, innerWidth, innerHeight, yForValue, chartData.baseline, baselineLabel, valueSuffix, valueDecimals, colors.baseline);
    }

    // Both series are drawn as points on a connecting line. Cycle lengths used
    // to be bars, and bar length encodes magnitude — which forces a zero-based
    // axis, where 28 d and 29 d are the same full-height bar. Position encoding
    // carries the same values honestly on a domain that separates them.
    drawValueLine(
      context,
      chartData.values,
      xForIndex,
      yForValue,
      colors.line,
      chartData.kind === "bar" ? HAIRLINE_WIDTH : SERIES_LINE_WIDTH
    );
    drawValuePoints(context, chartData.values, xForIndex, yForValue, colors.dot, colors.surface);

    if (isFiniteNumber(chartData.markerIndex)) {
      drawVerticalMarker(context, padding, innerHeight, xForIndex, chartData.markerIndex, chartData.markerLabel, colors.baseline);
    }
    drawXLabels(context, chartData.labels, xForIndex, size.height, padding, colors.label);
    drawYLabels(context, ticks, padding, yForValue, valueSuffix, valueDecimals, colors.label);
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
