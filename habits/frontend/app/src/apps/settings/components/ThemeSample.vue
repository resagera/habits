<script setup lang="ts">
// Образец темы: карточка, текст, кнопка и поле — теми же переменными, что и
// всё приложение. Отдельных цветов здесь нет: если образец выглядит хорошо,
// так же будет выглядеть и страница.
</script>

<template>
  <div class="sample">
    <div class="card">
      <p class="title">Заголовок карточки</p>
      <p class="text">Обычный текст — то, что читается чаще всего.</p>
      <p class="secondary">Второстепенный текст: подсказки и даты.</p>
      <div class="row">
        <button class="btn accent" type="button">Кнопка</button>
        <button class="btn" type="button">Обычная</button>
        <span class="cell">ячейка</span>
      </div>
      <input class="field" value="Поле ввода" readonly />
    </div>
  </div>
</template>

<style scoped>
.sample {
  margin: 8px 0 12px;
  border-radius: 10px;
  background: var(--bg-color);
  padding: 10px;
}

.card {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
}

.title {
  margin: 0 0 6px;
  font-size: 15px;
  color: var(--text-color);
}

.text {
  margin: 0 0 4px;
  font-size: 14px;
  color: var(--text-color);
}

.secondary {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--text-secondary);
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.btn {
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 6px 10px;
}

.btn.accent {
  background: var(--accent-color);
  color: #fff;
}

.cell {
  background: var(--cell-bg-color);
  border-radius: 6px;
  color: var(--text-secondary);
  font-size: 12px;
  padding: 5px 8px;
}

.field {
  width: 100%;
  font-size: 13px;
}
</style>
